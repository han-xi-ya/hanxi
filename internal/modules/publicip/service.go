package publicip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"hubkit/internal/platform"
)

// NetworkOverview 综合 IP 与网络详情
type NetworkOverview struct {
	// 出口公网信息
	PublicIPv4 string `json:"publicIpv4"`
	PublicIPv6 string `json:"publicIpv6"`
	SourceV4   string `json:"sourceV4"`
	SourceV6   string `json:"sourceV6"`
	FetchedAt  string `json:"fetchedAt"`

	// 本机物理与虚拟网卡详情列表
	Adapters []platform.Adapter `json:"adapters"`
}

type PublicIPService struct {
	plat   platform.Platform
	client *http.Client

	cacheMu    sync.RWMutex
	cachedData NetworkOverview
	cachedTime time.Time
	cacheTTL   time.Duration
}

func NewPublicIPService(plat platform.Platform) *PublicIPService {
	return &PublicIPService{
		plat:     plat,
		client:   &http.Client{Timeout: 3 * time.Second},
		cacheTTL: 2 * time.Minute, // 默认缓存 2 分钟
	}
}

var (
	// 国内 + 国际高可用 IPv4 查询源（覆盖家宽、专线、BGP）
	ipv4Providers = []string{
		"https://ip.3322.net",
		"https://ddns.oray.com/checkip",
		"https://api.ipify.org",
		"https://myip.ipip.net",
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
	}

	// IPv6 查询源
	ipv6Providers = []string{
		"https://speed.neu6.edu.cn/getIP.php",
		"https://v6.ident.me",
		"https://api6.ipify.org",
		"https://icanhazip.com",
	}

	ipRegex = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
)

// GetNetworkOverview 获取完整的出口公网 IP、局域网 IP、网关与 DNS 列表（2分钟内直接命中缓存）
func (s *PublicIPService) GetNetworkOverview(forceRefresh bool) (NetworkOverview, error) {
	// 1. 如果不是强制刷新，且在缓存有效期内，秒级直接返回
	if !forceRefresh {
		s.cacheMu.RLock()
		if !s.cachedTime.IsZero() && time.Since(s.cachedTime) < s.cacheTTL {
			data := s.cachedData
			s.cacheMu.RUnlock()
			return data, nil
		}
		s.cacheMu.RUnlock()
	}

	var overview NetworkOverview
	overview.FetchedAt = time.Now().Format("2006-01-02 15:04:05")

	var wg sync.WaitGroup
	wg.Add(3)

	// 1. 探测公网 IPv4
	go func() {
		defer wg.Done()
		for _, url := range ipv4Providers {
			ip, err := s.fetchIP(url, 4)
			if err == nil && ip != "" {
				overview.PublicIPv4 = ip
				overview.SourceV4 = url
				break
			}
		}
	}()

	// 2. 探测公网 IPv6
	go func() {
		defer wg.Done()
		for _, url := range ipv6Providers {
			ip, err := s.fetchIP(url, 6)
			if err == nil && ip != "" {
				overview.PublicIPv6 = ip
				overview.SourceV6 = url
				break
			}
		}
	}()

	// 3. 读取底层全部网卡、网关、DNS、临时 IPv6 详细信息
	go func() {
		defer wg.Done()
		adapters, err := s.plat.Network().Adapters()
		if err == nil {
			overview.Adapters = adapters
		}
	}()

	wg.Wait()

	// 写入缓存
	s.cacheMu.Lock()
	s.cachedData = overview
	s.cachedTime = time.Now()
	s.cacheMu.Unlock()

	return overview, nil
}

func (s *PublicIPService) fetchIP(targetURL string, expectFamily int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.0.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(string(body))

	if expectFamily == 4 {
		// 使用正则提取出其中的 IPv4 地址
		match := ipRegex.FindString(content)
		if match != "" && net.ParseIP(match).To4() != nil {
			return match, nil
		}
	} else if expectFamily == 6 {
		// IPv6 提取
		for _, token := range strings.Fields(content) {
			token = strings.Trim(token, "[](),:;\"'")
			parsed := net.ParseIP(token)
			if parsed != nil && parsed.To4() == nil && parsed.To16() != nil {
				return parsed.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no valid IP parsed from %s", targetURL)
}

// PingResult 单次 Ping 的返回结果
type PingResult struct {
	Seq      int     `json:"seq"`
	IP       string  `json:"ip"`
	RTTMs    float64 `json:"rttMs"`
	Success  bool    `json:"success"`
	TTL      int     `json:"ttl"`
	ErrorMsg string  `json:"errorMsg"`
}

// PingSummary Ping 统计概要
type PingSummary struct {
	Target       string       `json:"target"`
	IP           string       `json:"ip"`
	Sent         int          `json:"sent"`
	Received     int          `json:"received"`
	Lost         int          `json:"lost"`
	LossRate     float64      `json:"lossRate"`
	MinRTT       float64      `json:"minRtt"`
	MaxRTT       float64      `json:"maxRtt"`
	AvgRTT       float64      `json:"avgRtt"`
	Results      []PingResult `json:"results"`
}

// PingTarget 对目标域名或 IP 执行探测（默认探测 4 次）
func (s *PublicIPService) PingTarget(target string, count int) (PingSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return PingSummary{}, fmt.Errorf("目标地址不能为空")
	}
	if count <= 0 {
		count = 4
	}
	if count > 20 {
		count = 20
	}

	// 1. 解析目标 IP (IPv4)
	ips, err := net.LookupIP(target)
	if err != nil || len(ips) == 0 {
		return PingSummary{}, fmt.Errorf("无法解析目标主机: %s", target)
	}

	var targetIP net.IP
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			targetIP = ip4
			break
		}
	}
	if targetIP == nil {
		targetIP = ips[0]
	}
	ipStr := targetIP.String()

	summary := PingSummary{
		Target:  target,
		IP:      ipStr,
		Sent:    count,
		MinRTT:  999999,
		Results: make([]PingResult, 0, count),
	}

	var totalRTT float64
	for i := 1; i <= count; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		rtt, ok, err := s.plat.Network().Ping(ctx, ipStr, 1500*time.Millisecond)
		cancel()

		res := PingResult{
			Seq: i,
			IP:  ipStr,
		}

		if err != nil {
			res.Success = false
			res.ErrorMsg = err.Error()
		} else if ok {
			res.Success = true
			res.RTTMs = float64(rtt.Microseconds()) / 1000.0
			res.TTL = 64
			summary.Received++
			totalRTT += res.RTTMs

			if res.RTTMs < summary.MinRTT {
				summary.MinRTT = res.RTTMs
			}
			if res.RTTMs > summary.MaxRTT {
				summary.MaxRTT = res.RTTMs
			}
		} else {
			res.Success = false
			res.ErrorMsg = "请求超时"
		}

		summary.Results = append(summary.Results, res)
		if i < count {
			time.Sleep(200 * time.Millisecond)
		}
	}

	summary.Lost = summary.Sent - summary.Received
	summary.LossRate = (float64(summary.Lost) / float64(summary.Sent)) * 100.0
	if summary.Received > 0 {
		summary.AvgRTT = totalRTT / float64(summary.Received)
	} else {
		summary.MinRTT = 0
	}

	return summary, nil
}

// HopInfo 路由追踪单个跳跃节点
type HopInfo struct {
	Hop      int     `json:"hop"`
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	RTTMs    float64 `json:"rttMs"`
	Success  bool    `json:"success"`
}

// TracerouteSummary 路由追踪完整结果
type TracerouteSummary struct {
	Target   string    `json:"target"`
	IP       string    `json:"ip"`
	Hops     []HopInfo `json:"hops"`
	Complete bool      `json:"complete"`
}

// TraceRoute 执行路由追踪（基于 tracert / 本地探测，最大 30 跳）
func (s *PublicIPService) TraceRoute(target string, maxHops int) (TracerouteSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return TracerouteSummary{}, fmt.Errorf("目标地址不能为空")
	}
	if maxHops <= 0 {
		maxHops = 20
	}
	if maxHops > 30 {
		maxHops = 30
	}

	// 校验目标主机是否可解析
	ips, err := net.LookupIP(target)
	if err != nil || len(ips) == 0 {
		return TracerouteSummary{}, fmt.Errorf("无法解析目标主机: %s", target)
	}
	ipStr := ips[0].String()

	summary := TracerouteSummary{
		Target: target,
		IP:     ipStr,
		Hops:   make([]HopInfo, 0),
	}

	// 调用系统 tracert / traceroute
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tracert", "-d", "-h", fmt.Sprintf("%d", maxHops), "-w", "1000", target)
	} else {
		cmd = exec.Command("traceroute", "-n", "-m", fmt.Sprintf("%d", maxHops), "-w", "1", target)
	}
	hideWindow(cmd)

	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return summary, fmt.Errorf("执行路由追踪失败: %w", err)
	}

	// 解析输出行
	lines := strings.Split(string(out), "\n")

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Tracing") || strings.HasPrefix(l, "通过最多") || strings.HasPrefix(l, "Trace complete") || strings.HasPrefix(l, "追踪完成") {
			continue
		}

		parts := strings.Fields(l)
		if len(parts) < 2 {
			continue
		}

		var hopNum int
		if _, err := fmt.Sscanf(parts[0], "%d", &hopNum); err != nil || hopNum <= 0 {
			continue
		}

		// 检查行内是否超时
		if strings.Contains(l, "*") && !strings.Contains(l, "ms") {
			summary.Hops = append(summary.Hops, HopInfo{
				Hop:     hopNum,
				IP:      "*",
				Success: false,
			})
			continue
		}

		// 提取 IP 与最后一个 RTT
		lastPart := parts[len(parts)-1]
		if strings.HasSuffix(lastPart, "]") {
			lastPart = strings.Trim(lastPart, "[]")
		}

		var rtt float64
		for idx, p := range parts {
			if strings.Contains(p, "ms") && idx > 0 {
				valStr := strings.Trim(parts[idx-1], "<")
				var val float64
				if _, err := fmt.Sscanf(valStr, "%f", &val); err == nil {
					rtt = val
				}
			}
		}

		hopIP := lastPart
		if net.ParseIP(hopIP) == nil {
			// 从整行正则抽取
			matches := ipRegex.FindAllString(l, -1)
			if len(matches) > 0 {
				hopIP = matches[len(matches)-1]
			}
		}

		summary.Hops = append(summary.Hops, HopInfo{
			Hop:     hopNum,
			IP:      hopIP,
			RTTMs:   rtt,
			Success: hopIP != "*" && net.ParseIP(hopIP) != nil,
		})

		if hopIP == ipStr {
			summary.Complete = true
			break
		}
	}

	return summary, nil
}
