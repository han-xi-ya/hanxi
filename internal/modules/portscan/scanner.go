package portscan

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

const (
	DefaultScanConcurrency = 30
	MaxScanConcurrency     = 2000
	DefaultScanTimeout     = 600 * time.Millisecond
	DefaultEgressTimeout   = 3000 * time.Millisecond
	MaxPortNumber          = 65535
	MinPortNumber          = 1
)

// ContextDialer 抽象接口，统一标准 net.Dialer 和 proxy.ContextDialer
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

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

	// 高可用 IP 查询接口列表 (返回纯文本 IP)
	ipProviders := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
		"https://ip.3322.net",
		"https://ddns.oray.com/checkip",
	}

	var lastErr error
	for _, u := range ipProviders {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "curl/8.0.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := bufio.NewReader(resp.Body).ReadString('\n')
		resp.Body.Close()
		if err != nil && len(body) == 0 {
			lastErr = err
			continue
		}

		ipStr := strings.TrimSpace(body)
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.String(), nil
		}
		// 备用正则匹配
		matches := regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`).FindString(body)
		if matches != "" && net.ParseIP(matches) != nil {
			return matches, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("探测出网IP失败: %w", lastErr)
	}
	return "", fmt.Errorf("未能获取到有效的公网IP")
}

// createDialerAndHTTPClient 创建支持直接直连或 SOCKS5 / HTTP 代理的探测器
func (s *Scanner) createDialerAndHTTPClient(proxyStr string, timeout time.Duration) (ContextDialer, *http.Client, error) {
	baseDialer := &net.Dialer{
		Timeout: timeout,
	}

	var dialer ContextDialer = baseDialer
	proxyStr = strings.TrimSpace(proxyStr)

	var transport *http.Transport

	if proxyStr != "" {
		// 容错：如果用户只输入了 127.0.0.1:7890，默认当作 socks5 处理
		if !strings.Contains(proxyStr, "://") {
			proxyStr = "socks5://" + proxyStr
		}

		u, err := url.Parse(proxyStr)
		if err != nil {
			return nil, nil, fmt.Errorf("代理地址格式无效: %w", err)
		}

		switch strings.ToLower(u.Scheme) {
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				auth = &proxy.Auth{
					User: u.User.Username(),
				}
				if pwd, ok := u.User.Password(); ok {
					auth.Password = pwd
				}
			}
			socksDialer, err := proxy.SOCKS5("tcp", u.Host, auth, baseDialer)
			if err != nil {
				return nil, nil, fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
			}
			if cd, ok := socksDialer.(ContextDialer); ok {
				dialer = cd
			} else {
				dialer = &socks5ContextAdapter{dialer: socksDialer}
			}

			transport = &http.Transport{
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				DisableKeepAlives:     true,
				MaxIdleConns:          0,
				ResponseHeaderTimeout: timeout,
				DialContext:           dialer.DialContext,
			}

		case "http", "https":
			transport = &http.Transport{
				Proxy:                 http.ProxyURL(u),
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				DisableKeepAlives:     true,
				MaxIdleConns:          0,
				ResponseHeaderTimeout: timeout,
				DialContext:           baseDialer.DialContext,
			}
			// HTTP 代理直接使用 transport 的 DialContext（注意：常规 HTTP CONNECT 代理主要用于 HTTP 探测）
			dialer = baseDialer

		default:
			return nil, nil, fmt.Errorf("不支持的代理协议: %s (仅支持 socks5 / http)", u.Scheme)
		}
	} else {
		transport = &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives:     true,
			MaxIdleConns:          0,
			ResponseHeaderTimeout: timeout,
			DialContext:           baseDialer.DialContext,
		}
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   timeout * 2,
	}

	return dialer, httpClient, nil
}

type socks5ContextAdapter struct {
	dialer proxy.Dialer
}

func (a *socks5ContextAdapter) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := a.dialer.Dial(network, address)
		ch <- result{conn: conn, err: err}
	}()

	select {
	case <-ctx.Done():
		// 若 context 取消或超时，启动后台接收 Goroutine，在连接建立成功后立即关闭，防止 socket 孤立泄露
		go func() {
			res := <-ch
			if res.conn != nil {
				_ = res.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case res := <-ch:
		return res.conn, res.err
	}
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
	bufPool    = sync.Pool{
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
	var lastEmitTime int64

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

					var latestOpen *PortResult
					if res.Status == PortOpen {
						atomic.AddInt64(&foundOpenCount, 1)
						mu.Lock()
						openPorts = append(openPorts, res)
						mu.Unlock()
						latestOpen = &res
					}

					if progressCallback != nil && ctx.Err() == nil {
						now := time.Now().UnixNano()
						last := atomic.LoadInt64(&lastEmitTime)
						shouldEmit := (latestOpen != nil) || (now-last >= int64(100*time.Millisecond)) || (curScanned == int64(total))

						if shouldEmit && atomic.CompareAndSwapInt64(&lastEmitTime, last, now) {
							pct := float64(curScanned) / float64(total) * 100
							progressCallback(ScanProgress{
								TaskID:     taskID,
								Target:     target,
								Scanned:    int(curScanned),
								Total:      total,
								Percent:    pct,
								FoundOpen:  int(atomic.LoadInt64(&foundOpenCount)),
								LatestPort: latestOpen,
								IsFinished: false,
							})
						}
					}
				}
			}
		}()
	}

	wg.Wait()

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

		if n > 0 {
			greeting := string(bytes.TrimSpace(buf[:n]))
			bufPool.Put(bufPtr)
			if strings.HasPrefix(greeting, "SSH-") {
				res.Service = "ssh"
				res.Banner = greeting
				return
			} else if strings.HasPrefix(greeting, "+PONG") || strings.Contains(greeting, "NOAUTH") || strings.Contains(greeting, "-ERR") {
				res.Service = "redis"
				res.Banner = "Redis Server"
				return
			} else if strings.HasPrefix(greeting, "220") {
				if strings.Contains(strings.ToLower(greeting), "ftp") {
					res.Service = "ftp"
				} else {
					res.Service = "smtp"
				}
				res.Banner = greeting
				return
			} else if n > 5 && (bytes.Contains(buf[:n], []byte("mysql")) || bytes.Contains(buf[:n], []byte("MariaDB"))) {
				res.Service = "mysql"
				res.Banner = "MySQL / MariaDB"
				return
			}
		} else {
			bufPool.Put(bufPtr)
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
		req.Header.Set("User-Agent", "Mozilla/5.0 (HubKit)")
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
