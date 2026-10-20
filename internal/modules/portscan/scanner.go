package portscan

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/packages/go/netx"
)

const (
	DefaultScanConcurrency = 30
	MaxScanConcurrency     = 2000
	DefaultScanTimeout     = 600 * time.Millisecond
	DefaultEgressTimeout   = 3000 * time.Millisecond
	MaxPortNumber          = 65535
	MinPortNumber          = 1

	// progressEmitInterval 纯进度心跳的最小发射间隔。心跳事件可安全合批/丢弃——
	// 前端只是覆盖刷新进度数字，终态 summary 兜底最终值，丢发无数据损失。
	progressEmitInterval = 100 * time.Millisecond

	// openMergeWindow 开放端口事件的合并派发窗口。
	// 定为心跳窗的一半（50ms）：单端口发现的推送延迟肉眼无感（<0.1s），突发时
	// 事件量封顶 20 条/秒（叠加心跳 10 条/秒 = 30 条/秒），与前端"每事件追加一个
	// 端口、按端口号去重"的上屏模型对齐；超出窗口的命中合批（FoundOpen 计数恒准），
	// 未逐个上屏的端口由终态 summary 全量补齐。
	openMergeWindow = 50 * time.Millisecond
)

// ContextDialer 统一标准 net.Dialer 与 SOCKS 拨号器的最小接口。
// 代理拨号内核已上收至 netx（portscan 与 proxycheck 共用一份实现），此处只留
// 类型别名维持包内既有签名不变——勿改回本地定义，那会把实现重新劈成两份。
type ContextDialer = netx.ContextDialer

// ParsePortRange 解析诸如 "80,443,8000-8005" 的端口字符串，返回去重有序的端口切片
func ParsePortRange(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("端口范围不能为空")
	}

	seen := make(map[int]bool)
	var ports []int

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("无效的起始端口: %s", rangeParts[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("无效的结束端口: %s", rangeParts[1])
			}
			if start > end {
				start, end = end, start
			}
			if start < MinPortNumber || end > MaxPortNumber {
				return nil, fmt.Errorf("端口超出合法范围 (1-65535): %d-%d", start, end)
			}
			for p := start; p <= end; p++ {
				if !seen[p] {
					seen[p] = true
					ports = append(ports, p)
				}
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("无效的端口: %s", part)
			}
			if p < MinPortNumber || p > MaxPortNumber {
				return nil, fmt.Errorf("端口超出合法范围 (1-65535): %d", p)
			}
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}

	if len(ports) == 0 {
		return nil, fmt.Errorf("未解析出有效端口")
	}

	sort.Ints(ports)
	return ports, nil
}

// Scanner 纯原生极轻量端口扫描引擎 (0 外部重依赖，0 堆常驻内存)
type Scanner struct{}

// NewScanner 创建无状态扫描引擎，所有方法可并发调用。
func NewScanner() *Scanner {
	return &Scanner{}
}

// QueryEgressIP 测试指定代理或直连下的实际出网 IP
func (s *Scanner) QueryEgressIP(ctx context.Context, proxyURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = DefaultEgressTimeout
	}

	_, httpClient, err := s.createDialerAndHTTPClient(proxyURL, timeout)
	if err != nil {
		return "", err
	}

	// 多源降级列表、响应体上限与"不跟跳转"的护栏都归 netx.QueryEgress；
	// 本层只保留"返回单个 IP 字符串"的既有对外形态（前端出网 IP 条依赖它）。
	ip, _, err := netx.QueryEgress(ctx, httpClient, netx.EgressProvidersV4)
	return ip, err
}

// createDialerAndHTTPClient 建出直连或过 SOCKS5 / HTTP(S) 代理的探测器。
//
// 实现已上收 netx.ParseProxy + netx.DialProxy（portscan 与 proxycheck 共用同一份
// 代理拨号内核，N25「消灭孤例」口径），本层只补两条**模块专属**口径：
//   - TLSInsecureSkipVerify 恒 true：扫描只问"靶活着没"，证书链不参与存活判定。
//     proxycheck 要答的是"证书有没有被中间网关替换"，那是它自己的问题，勿在这里
//     "顺手统一"成验证书——那会让 portscan 的 HTTPS 靶大面积误报为不可达。
//   - 返回的 dialer 对 HTTP(S) 代理**仍是直连拨号器**：标准库的 CONNECT 隧道只作用
//     于经该 http.Client 发出的请求，而 probePort 的裸 TCP 探测走 dialer。这是在册
//     既有行为（详见 netx.DialProxy 函数注释），用户配 HTTP 代理时端口探测探的就是
//     直达靶，PRD 里"SOCKS5/HTTP 代理扫描"那行的含义随它定，勿改。
func (s *Scanner) createDialerAndHTTPClient(proxyStr string, timeout time.Duration) (ContextDialer, *http.Client, error) {
	p, err := netx.ParseProxy(proxyStr)
	if err != nil {
		return nil, nil, err
	}
	return netx.DialProxy(p, netx.DialOpts{Timeout: timeout, TLSInsecureSkipVerify: true})
}

// KnownPortServices 常用端口默认服务推断表
var KnownPortServices = map[int]string{
	21:    "ftp",
	22:    "ssh",
	23:    "telnet",
	25:    "smtp",
	53:    "dns",
	80:    "http",
	110:   "pop3",
	135:   "msrpc",
	139:   "netbios-ssn",
	143:   "imap",
	443:   "https",
	445:   "microsoft-ds",
	1080:  "socks5",
	1433:  "ms-sql-s",
	1521:  "oracle",
	3000:  "http(dev)",
	3306:  "mysql",
	3389:  "ms-wbt-server(rdp)",
	5000:  "http(dev)",
	5173:  "vite(http)",
	5432:  "postgresql",
	5900:  "vnc",
	6379:  "redis",
	7000:  "frps",
	7001:  "frps-dashboard",
	8000:  "http-alt",
	8080:  "http-proxy",
	8081:  "http-alt",
	8443:  "https-alt",
	8888:  "http-alt",
	9000:  "http-alt",
	9200:  "elasticsearch",
	27017: "mongodb",
}

var (
	titleRegex = regexp.MustCompile(`(?i)<title>(.*?)</title>`)
	// 出网 IP 的兜底抠取随内核一起上收 netx（egressIPv4Re），此处不留第二份。
	bufPool = sync.Pool{
		New: func() any {
			b := make([]byte, 512)
			return &b
		},
	}
)

// ExecuteScan 运行高并发扫描任务
func (s *Scanner) ExecuteScan(
	ctx context.Context,
	taskID string,
	target string,
	ports []int,
	proxyURL string,
	timeout time.Duration,
	concurrency int,
	rateLimitMs int,
	deepDetect bool,
	progressCallback func(p ScanProgress),
) (*ScanSummary, error) {
	if concurrency <= 0 {
		concurrency = DefaultScanConcurrency
	}
	if concurrency > MaxScanConcurrency {
		concurrency = MaxScanConcurrency
	}
	if timeout <= 0 {
		timeout = DefaultScanTimeout
	}

	dialer, httpClient, err := s.createDialerAndHTTPClient(proxyURL, timeout)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	total := len(ports)
	var scannedCount int64
	var foundOpenCount int64
	var lastEmitTime int64 // 心跳闸门：只限流"纯进度刷新"，开放事件不走此闸

	// 开放端口事件与进度心跳彻底分闸（缺陷留痕：旧实现两类事件共用单一 CAS(lastEmitTime)
	// 闸门，worker 竞争失败时"新发现开放端口"被静默丢发；前端只靠 latestPort 增量上屏，
	// 丢发=扫描进行中该端口不显示，终态 summary 才回补——"新发现"与"心跳进度"混用
	// 一条节流道的典型事故）。
	// 新通道语义：开放端口是必达事件——worker 命中后只写入 pendingOpen 槽位（与心跳闸
	// 无竞争，不存在丢发路径），由单派发协程按 openMergeWindow 节拍独立发射；
	// 突发（如整段 1-1024 全开）下槽位"保最新即合批"，事件量封顶 20 条/秒且
	// FoundOpen 计数恒准，被合掉的端口不丢——它们同批已进 openPorts 汇总集，
	// 终态 summary 全量兜底送达（不丢端口、可合批）。
	var pendingOpen atomic.Pointer[PortResult]

	// emitOpenProgress 构造并发射一条携带开放端口增量的事件，
	// 进度字段取发射瞬间的原子快照（与心跳事件同源同形，前端无需区分）。
	emitOpenProgress := func(res *PortResult) {
		cur := atomic.LoadInt64(&scannedCount)
		progressCallback(ScanProgress{
			TaskID:     taskID,
			Target:     target,
			Scanned:    int(cur),
			Total:      total,
			Percent:    float64(cur) / float64(total) * 100,
			FoundOpen:  int(atomic.LoadInt64(&foundOpenCount)),
			LatestPort: res,
			IsFinished: false,
		})
	}

	stopDispatch := make(chan struct{})
	dispatchDone := make(chan struct{})
	if progressCallback != nil {
		go func() {
			defer close(dispatchDone)
			ticker := time.NewTicker(openMergeWindow)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopDispatch:
					// 扫描收尾前末次补发：让最新一个未播报的开放端口赶在终态事件
					// 之前进入事件流（派发 join 先于 finished 事件，序关系固定）
					if res := pendingOpen.Swap(nil); res != nil && ctx.Err() == nil {
						emitOpenProgress(res)
					}
					return
				case <-ticker.C:
					if res := pendingOpen.Swap(nil); res != nil && ctx.Err() == nil {
						emitOpenProgress(res)
					}
				}
			}
		}()
	}

	var (
		mu        sync.Mutex
		openPorts []PortResult
	)

	// 任务池队列
	portChan := make(chan int, 4096)
	go func() {
		defer close(portChan)
		for _, p := range ports {
			select {
			case <-ctx.Done():
				return
			case portChan <- p:
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p, ok := <-portChan:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}

					// 微延迟防封机制
					if rateLimitMs > 0 {
						time.Sleep(time.Duration(rateLimitMs) * time.Millisecond)
					}

					res := s.probePort(ctx, dialer, httpClient, target, p, timeout, deepDetect)
					curScanned := atomic.AddInt64(&scannedCount, 1)

					if res.Status == PortOpen {
						atomic.AddInt64(&foundOpenCount, 1)
						mu.Lock()
						openPorts = append(openPorts, res)
						mu.Unlock()
						if progressCallback != nil {
							// 必达投递：写独立槽位（保最新=合批），与心跳闸门零竞争；
							// 每轮 worker 持独立 res 副本，逃逸分配上界=开放端口数(≤65535)
							open := res
							pendingOpen.Store(&open)
						}
					}

					// 纯进度心跳：只走自己的限流闸，竞争失败仅意味着"少刷一次数字"，
					// 不携带也不消耗任何开放端口数据。
					if progressCallback != nil && ctx.Err() == nil {
						now := time.Now().UnixNano()
						last := atomic.LoadInt64(&lastEmitTime)
						shouldEmit := (now-last >= int64(progressEmitInterval)) || (curScanned == int64(total))

						if shouldEmit && atomic.CompareAndSwapInt64(&lastEmitTime, last, now) {
							pct := float64(curScanned) / float64(total) * 100
							progressCallback(ScanProgress{
								TaskID:     taskID,
								Target:     target,
								Scanned:    int(curScanned),
								Total:      total,
								Percent:    pct,
								FoundOpen:  int(atomic.LoadInt64(&foundOpenCount)),
								IsFinished: false,
							})
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	// 先收开放事件派发协程（join 于终态事件之前）：杜绝扫描结束后再窜出迟到的
	// latestPort 事件打乱前端"进度已 100%"的收尾语义；无回调的无头路径（MCP）
	// 未起派发协程，无需收。
	if progressCallback != nil {
		close(stopDispatch)
		<-dispatchDone
	}

	// 排序开放端口结果
	mu.Lock()
	sort.Slice(openPorts, func(i, j int) bool {
		return openPorts[i].Port < openPorts[j].Port
	})
	finalOpen := make([]PortResult, len(openPorts))
	copy(finalOpen, openPorts)
	mu.Unlock()

	summary := &ScanSummary{
		TaskID:     taskID,
		Target:     target,
		TotalPorts: total,
		OpenPorts:  finalOpen,
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	if progressCallback != nil && ctx.Err() == nil {
		progressCallback(ScanProgress{
			TaskID:     taskID,
			Target:     target,
			Scanned:    total,
			Total:      total,
			Percent:    100,
			FoundOpen:  len(finalOpen),
			IsFinished: true,
		})
	}

	return summary, nil
}

// probePort 探测单个端口
func (s *Scanner) probePort(ctx context.Context, dialer ContextDialer, httpClient *http.Client, target string, port int, timeout time.Duration, deepDetect bool) PortResult {
	if ctx.Err() != nil {
		return PortResult{Port: port, Status: PortClosed}
	}

	addr := net.JoinHostPort(target, strconv.Itoa(port))
	start := time.Now()

	// 1. TCP 建立连接探测
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return PortResult{
			Port:      port,
			Status:    PortClosed,
			LatencyMs: latency,
		}
	}
	_ = conn.Close()

	// 端口已开放
	res := PortResult{
		Port:      port,
		Status:    PortOpen,
		LatencyMs: latency,
		Service:   KnownPortServices[port],
	}
	if res.Service == "" {
		res.Service = "unknown"
	}

	// 2. 如果开启服务探测，并且 ctx 未取消
	if deepDetect && ctx.Err() == nil {
		s.lightweightProbe(ctx, dialer, httpClient, target, port, timeout, &res)
	}

	return res
}

// lightweightProbe 极轻量探针，快速提取服务特征
func (s *Scanner) lightweightProbe(ctx context.Context, dialer ContextDialer, httpClient *http.Client, target string, port int, timeout time.Duration, res *PortResult) {
	if ctx.Err() != nil {
		return
	}
	addr := net.JoinHostPort(target, strconv.Itoa(port))

	// 1. 尝试主动握手/被动 Banner 探测 (SSH / FTP / SMTP / MySQL / Redis)
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(timeout))

		if port == 6379 {
			if _, err := conn.Write([]byte("PING\r\n")); err != nil {
				_ = conn.Close()
				return
			}
		}

		bufPtr := bufPool.Get().(*[]byte)
		buf := *bufPtr
		n, _ := conn.Read(buf)
		_ = conn.Close()

		// 分支判定必须在归还缓冲区之前全部完成：MySQL 分支直读原始字节 buf[:n]，
		// 若先 Put 再读，池可能已把同一底层数组交给另一个 goroutine（读写竞争）。
		// 因此这里"读完 → 判完 → 最后统一 Put"，命中判定用 matched 收口而非分散 return。
		matched := false
		if n > 0 {
			greeting := string(bytes.TrimSpace(buf[:n]))
			switch {
			case strings.HasPrefix(greeting, "SSH-"):
				res.Service = "ssh"
				res.Banner = greeting
				matched = true
			case strings.HasPrefix(greeting, "+PONG") || strings.Contains(greeting, "NOAUTH") || strings.Contains(greeting, "-ERR"):
				res.Service = "redis"
				res.Banner = "Redis Server"
				matched = true
			case strings.HasPrefix(greeting, "220"):
				if strings.Contains(strings.ToLower(greeting), "ftp") {
					res.Service = "ftp"
				} else {
					res.Service = "smtp"
				}
				res.Banner = greeting
				matched = true
			case n > 5 && (bytes.Contains(buf[:n], []byte("mysql")) || bytes.Contains(buf[:n], []byte("MariaDB"))):
				res.Service = "mysql"
				res.Banner = "MySQL / MariaDB"
				matched = true
			}
		}
		bufPool.Put(bufPtr)
		if matched {
			return
		}
	}

	if ctx.Err() != nil || httpClient == nil {
		return
	}

	// 2. HTTP / HTTPS 标题与 Server 头探测
	isTLS := (port == 443 || port == 8443)
	schema := "http"
	if isTLS {
		schema = "https"
	}
	url := fmt.Sprintf("%s://%s:%d/", schema, target, port)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Hanxi)")
		resp, err := httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			res.Service = schema

			server := resp.Header.Get("Server")
			scanner := bufio.NewScanner(resp.Body)
			var bodyBuf bytes.Buffer
			for scanner.Scan() {
				bodyBuf.Write(scanner.Bytes())
				if bodyBuf.Len() > 2048 {
					break
				}
			}
			matches := titleRegex.FindSubmatch(bodyBuf.Bytes())
			title := ""
			if len(matches) > 1 {
				title = strings.TrimSpace(string(matches[1]))
			}

			parts := []string{}
			if server != "" {
				parts = append(parts, server)
			}
			if title != "" {
				parts = append(parts, fmt.Sprintf("Title: %s", title))
			}
			if len(parts) > 0 {
				res.Banner = strings.Join(parts, " | ")
			}
		}
	}
}
