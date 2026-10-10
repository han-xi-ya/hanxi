package publicip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"

	"hanxi/packages/go/netx"
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

// PublicIPService 公网/局域网信息聚合服务。
// 对上游查询源做进程内 TTL 缓存（RWMutex 读写锁）：TTL 内的重复请求直接回吐 cachedData，
// 前端轮询不放大外部请求量；缓存含部分失败结果（某源挂掉不会立即重查），
// 但 IPv4/IPv6 两族全源皆败的"空结果"不写缓存——失败不配被钉死 2 分钟，
// 下一次调用照常重试（2026-09-27 缺陷修复：此前吞错并缓存空结果，前端持续空显）。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）。
type PublicIPService struct {
	plat   platform.Platform
	client *http.Client
	holder *extapi.LeaseHolder

	cacheMu    sync.RWMutex
	cachedData NetworkOverview
	cachedTime time.Time
	cacheTTL   time.Duration
}

// NewPublicIPService 创建服务：单源 HTTP 超时 3s（多源轮询兜底），结果缓存 2 分钟。
//
// N25 语义决策：出网走 netx 代理链（机主 2026-09-23 扩面点名"publicip 等在线
// 查询"随浏览器同进退）。诚实边界——开启系统代理时，探得的"公网 IP"实为**代理
// 出口 IP**而非本机裸出口 IP；这正是"看我现在对外是什么地址"的期望语义，前端
// 文案不得谎称"本机真实出口"。若要裸出口需另立直连探针（本次不做，避免双出口混淆）。
func NewPublicIPService(plat platform.Platform, holder *extapi.LeaseHolder) *PublicIPService {
	return &PublicIPService{
		plat:     plat,
		client:   netx.NewClient(3*time.Second, nil),
		holder:   holder,
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

// GetNetworkOverview 获取完整的出口公网 IP、局域网 IP、网关与 DNS 列表（2分钟内直接命中缓存）。
//
// 全源失败如实上报口径（2026-09-27 缺陷修复）：此前 IPv4 六源 + IPv6 四源全部失败时
// 吞掉各源错误、不打日志、返回零值"成功"并把空结果写进缓存——前端空显且 2 分钟内
// 重进仍读空缓存。现改为：
//   - 每个源的最后一次错误都保留并以 slog.Warn 落日志（errors.Join 聚合，可定位"哪个源怎么挂的"）；
//   - 两族全挂 → 返回错误（前端既有 catch 路径如实显示"获取网络配置信息失败"），且**不写缓存**，
//     下一次调用正常重试；
//   - 任一族成功即视为正常结果并缓存（家宽普遍无 IPv6，v6 全挂属常态而非故障——该情形只按
//     Info 级留痕；反之 v4 全挂仅 v6 命中按 Warn 留痕）。NetworkOverview 字段与 JSON 形态不变，
//     前端沿用"publicIpv6 为空即未获得"的既有表达，不新增协议字段。
//   - 网卡枚举失败不升级为整体错误（本地详情与出网探测正交），单独 slog.Warn 留痕。
func (s *PublicIPService) GetNetworkOverview(forceRefresh bool) (NetworkOverview, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return NetworkOverview{}, gateErr
	}
	defer release()

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

	// 三个 goroutine 各写独立的字段与独立的错误变量，互不重叠，wg.Wait 建立可见性
	var v4Err, v6Err, adaptersErr error

	// 1. 探测公网 IPv4
	go func() {
		defer wg.Done()
		ip, source, err := s.fetchFamily(ipv4Providers, 4)
		if err != nil {
			v4Err = err
			return
		}
		overview.PublicIPv4 = ip
		overview.SourceV4 = source
	}()

	// 2. 探测公网 IPv6
	go func() {
		defer wg.Done()
		ip, source, err := s.fetchFamily(ipv6Providers, 6)
		if err != nil {
			v6Err = err
			return
		}
		overview.PublicIPv6 = ip
		overview.SourceV6 = source
	}()

	// 3. 读取底层全部网卡、网关、DNS、临时 IPv6 详细信息
	go func() {
		defer wg.Done()
		adapters, err := s.plat.Network().Adapters()
		if err != nil {
			adaptersErr = err
			return
		}
		overview.Adapters = adapters
	}()

	wg.Wait()

	if adaptersErr != nil {
		slog.Warn("publicip: 网卡枚举失败（本次本机详情缺省）", "err", adaptersErr)
	}

	// 2. 两族全挂：如实上报错误，不写缓存（失败结果入缓存会把空展示钉死 2 分钟）
	if v4Err != nil && v6Err != nil {
		allErr := errors.Join(v4Err, v6Err)
		slog.Warn("publicip: 公网 IP 探测全源失败（IPv4+IPv6），已如实上报且不写入缓存", "err", allErr)
		return NetworkOverview{}, fmt.Errorf("公网 IP 探测全部失败: %w", allErr)
	}

	// 单族失败只留痕不报错：v6 无属常态（Info），v4 无属值得警惕（Warn）
	switch {
	case v6Err != nil:
		slog.Info("publicip: IPv6 全源探测失败（未开通/被拦截属常态，前端按空值表达未获得）", "err", v6Err)
	case v4Err != nil:
		slog.Warn("publicip: IPv4 全源探测失败（仅 IPv6 命中的降级成功）", "err", v4Err)
	}

	// 3. 写入缓存（成功或单族成功的降级结果）
	s.cacheMu.Lock()
	s.cachedData = overview
	s.cachedTime = time.Now()
	s.cacheMu.Unlock()

	return overview, nil
}

// fetchFamily 顺序轮询同族全部查询源，首个成功即返回其 IP 与来源 URL；
// 全源皆败时把每个源的错误（含源 URL）以 errors.Join 聚合返回——绝不静默吞错。
func (s *PublicIPService) fetchFamily(providers []string, family int) (ip, source string, err error) {
	var errs []error
	for _, url := range providers {
		got, err := s.fetchIP(url, family)
		if err == nil && got != "" {
			return got, url, nil
		}
		if err == nil {
			err = errors.New("返回空结果")
		}
		errs = append(errs, fmt.Errorf("%s: %w", url, err))
	}
	joined := errors.Join(errs...)
	if joined == nil {
		// 防御：源列表为空同样按失败上报，绝不回 nil 错误+空 IP 的"假成功"
		joined = errors.New("该族未配置任何查询源")
	}
	return "", "", joined
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
	Seq     int     `json:"seq"`
	IP      string  `json:"ip"`
	RTTMs   float64 `json:"rttMs"`
	Success bool    `json:"success"`
	// TTL 生存时间：platform 层 Ping 契约只回 RTT 不回 TTL，如实置零；
	// 配 omitempty 使零值不出现在 JSON——前端 TTL 列成功行留白而非显示编造的
	// "64" 或误导性的 "0"（2026-09-27 修复：删除此前硬编码 res.TTL = 64）。
	// 升级路径见 PingTarget 内 TODO(波2)。
	TTL      int    `json:"ttl,omitempty"`
	ErrorMsg string `json:"errorMsg"`
}

// PingSummary Ping 统计概要
type PingSummary struct {
	Target   string       `json:"target"`
	IP       string       `json:"ip"`
	Sent     int          `json:"sent"`
	Received int          `json:"received"`
	Lost     int          `json:"lost"`
	LossRate float64      `json:"lossRate"`
	MinRTT   float64      `json:"minRtt"`
	MaxRTT   float64      `json:"maxRtt"`
	AvgRTT   float64      `json:"avgRtt"`
	Results  []PingResult `json:"results"`
}

// PingTarget 对目标域名或 IP 执行探测（默认探测 4 次）
func (s *PublicIPService) PingTarget(target string, count int) (PingSummary, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return PingSummary{}, gateErr
	}
	defer release()

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
			// TTL 不编数：此前硬编码 res.TTL = 64，platform Ping 根本不回 TTL，
			// UI 呈现"实测 TTL 64"违反"不猜不编"口径（2026-09-27 修复，如实置零）。
			// TODO(波2): platform.NetworkAPI.Ping 扩为返回实测 TTL 后，在此回填真值。
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return TracerouteSummary{}, gateErr
	}
	defer release()

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

	// 调用系统 tracert（Windows-only 项目，不再保留 traceroute 分支）
	cmd := exec.Command("tracert", "-d", "-h", fmt.Sprintf("%d", maxHops), "-w", "1000", target)
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
