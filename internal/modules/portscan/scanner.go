package portscan

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
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
)

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
			if start < 1 || end > 65535 {
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
			if p < 1 || p > 65535 {
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

// Scanner 纯原生极轻量端口扫描引擎 (0 外部依赖，0 堆常驻内存)
type Scanner struct{}

func NewScanner() *Scanner {
	return &Scanner{}
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

var titleRegex = regexp.MustCompile(`(?i)<title>(.*?)</title>`)

// ExecuteScan 运行高并发扫描任务
func (s *Scanner) ExecuteScan(
	ctx context.Context,
	taskID string,
	target string,
	ports []int,
	timeout time.Duration,
	concurrency int,
	deepDetect bool,
	progressCallback func(p ScanProgress),
) (*ScanSummary, error) {
	if concurrency <= 0 {
		concurrency = 100
	}
	if concurrency > 500 {
		concurrency = 500
	}
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}

	startTime := time.Now()
	total := len(ports)
	var scannedCount int64
	var foundOpenCount int64

	var (
		mu        sync.Mutex
		openPorts []PortResult
	)

	// 任务池队列
	portChan := make(chan int, concurrency*2)
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
					res := s.probePort(ctx, target, p, timeout, deepDetect)
					curScanned := atomic.AddInt64(&scannedCount, 1)

					var latestOpen *PortResult
					if res.Status == PortOpen {
						atomic.AddInt64(&foundOpenCount, 1)
						mu.Lock()
						openPorts = append(openPorts, res)
						mu.Unlock()
						latestOpen = &res
					}

					if progressCallback != nil {
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

	if progressCallback != nil {
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
func (s *Scanner) probePort(ctx context.Context, target string, port int, timeout time.Duration, deepDetect bool) PortResult {
	addr := net.JoinHostPort(target, strconv.Itoa(port))
	start := time.Now()

	// 1. TCP 快速建立连接探测
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
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

	// 2. 极轻量原生应用层指纹探针（毫秒级，0 内存占用）：HTTP Title / SSH / Redis / MySQL / SMTP / FTP
	if deepDetect {
		s.lightweightProbe(ctx, target, port, timeout, &res)
	}

	return res
}

// lightweightProbe 极轻量原生探针，快速提取常见服务特征，0 内存常驻
func (s *Scanner) lightweightProbe(ctx context.Context, target string, port int, timeout time.Duration, res *PortResult) {
	addr := net.JoinHostPort(target, strconv.Itoa(port))

	// 1. 尝试主动握手/被动 Banner 探测 (SSH / FTP / SMTP / MySQL)
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(timeout))

		// 如果是 Redis 端口或未知端口，可尝试发送 PING\r\n
		if port == 6379 {
			_, _ = conn.Write([]byte("PING\r\n"))
		}

		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		_ = conn.Close()

		if n > 0 {
			greeting := string(bytes.TrimSpace(buf[:n]))
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
		}
	}

	// 2. HTTP / HTTPS 标题与 Server 头探测
	isTLS := (port == 443 || port == 8443)
	schema := "http"
	if isTLS {
		schema = "https"
	}
	url := fmt.Sprintf("%s://%s:%d/", schema, target, port)

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (HubKit)")
		resp, err := client.Do(req)
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
