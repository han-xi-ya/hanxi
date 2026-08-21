package publicip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
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
