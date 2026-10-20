package portscan

// 代理连通性测试：填一条代理，回答"能不能用"，能用的顺带报出口 IP。
//
// 判定口径就一条实用定义：**能经这条代理拿到公网出口 IP，才算通**。它同时覆盖了
// "TCP 连不上""端口开着但不是代理""要认证而没给凭据""代理能握手但转发不出去"——
// 这几种在前端呈现相同（不通 + 一行真实原因），原因由底层给，不做归类枚举。
//
// 拨号与出口核验全部复用 netx 的显式代理内核（与扫描中继是同一份实现），本文件
// 只做参数校验、地址拼装与结果整形。刻意不做批量：单条判定才是这个工具的用途。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/packages/go/netx"
)

const (
	// ProxyCheckDefaultTimeoutMs 单条代理的总预算（连接 + TLS + 一轮出口核验）。
	ProxyCheckDefaultTimeoutMs = 5000
	// ProxyCheckMinTimeoutMs / Max 是给 UI 越界输入的兜底：预算太短会把好代理测成
	// 不通，太长则一次卡住要等很久，两头都拦一下而不是静默按原值跑。
	ProxyCheckMinTimeoutMs = 500
	ProxyCheckMaxTimeoutMs = 30000
)

// ProxyKindHTTP 等取值与 netx.ProxyKind 对齐；这里不收枚举校验，交给 ParseProxy 一处判。
// ProxyCheckRequest 一次测试的入参（结构化字段，不由前端拼 URL）。
type ProxyCheckRequest struct {
	// Kind 是协议方言：socks5 / http / https。
	Kind string `json:"kind"`
	// Host 只含主机或 IP，不带端口、不带凭据。
	Host string `json:"host"`
	Port int    `json:"port"`
	// User / Pass 可选。**只用于这一次拨号，不落盘、不回显、不进结果**。
	User string `json:"user"`
	Pass string `json:"pass"`
	// TimeoutMs 单条预算，留空用 ProxyCheckDefaultTimeoutMs。
	TimeoutMs int `json:"timeoutMs"`
	// SkipTLSVerify 跳过端到端那轮的证书校验。默认 false：代理链路上有透明网关
	// 换证书时，这里就该如实判为不通；内网自签代理才需要勾。
	SkipTLSVerify bool `json:"skipTlsVerify"`
}

// ProxyCheckResult 单条代理的结果。
//
// Addr 恒为 netx 的剥凭据规范形（由类型层保证，不靠调用方记得剥）——所以结果要
// 进前端、被复制、进日志都不漏口令；凭据只活在 ParsedProxy 的非导出字段里，
// 供 DialProxy 拨号时取用。
type ProxyCheckResult struct {
	Addr         string `json:"addr"`
	OK           bool   `json:"ok"`
	EgressIP     string `json:"egressIp,omitempty"`
	EgressSource string `json:"egressSource,omitempty"`
	// EgressAgree / EgressSources：多少个源给出了与 EgressIP 相同的值 / 成功取到 IP 的
	// 源总数。3/4 与 4/4 的可信度不同，这个分歧本身就是要呈现给用户的信息。
	EgressAgree   int    `json:"egressAgree"`
	EgressSources int    `json:"egressSources"`
	LatencyMs     int64  `json:"latencyMs"`
	Error         string `json:"error,omitempty"`
	// Probes 是**每个**出口源的独立结果（全部显示，不只是判通那一发）。
	// 为什么值得摊开：多源给出不同 IP 是常态（源自身多机房、NAT 池轮转、线路被
	// 区别对待），只报第一个成功值会让人怀疑工具测错；并排显示才看得出真相。
	Probes []EgressProbe `json:"probes,omitempty"`
}

// EgressProbe 单个出口源的查询结果。
type EgressProbe struct {
	Source    string `json:"source"`
	IP        string `json:"ip,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs"`
}

// proxyCheckEgressProviders 端到端那一轮打的出口核验源。
// 做成包级变量而非直接引用常量表：单测要能换成 httptest 本地服务——CI 不碰外网，
// 也不该让"测一条代理"真的往公网发请求。测试之外不得改写。
var proxyCheckEgressProviders = netx.EgressProvidersV4

// CheckProxy 测一条代理。返回的 error 只表示"这次没能开测"（参数不合法等），
// 测了但不通是正常的 OK=false + Error 文本，不当错误上抛。
func (s *Scanner) CheckProxy(ctx context.Context, req ProxyCheckRequest) (*ProxyCheckResult, error) {
	addr, err := req.canonicalAddr()
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(req.effectiveTimeoutMs()) * time.Millisecond
	p, err := netx.ParseProxy(addr)
	if err != nil {
		// 这条错误文本可能内嵌原始 URL（含密码），抹净后再上抛
		return nil, errors.New(redactSecret(err.Error(), req.Pass))
	}

	res := &ProxyCheckResult{Addr: p.String()}

	// 必须把 ParsedProxy **本体**交给 DialProxy：凭据藏在它的非导出字段里。若拿
	// p.String()（剥凭据形）重新解析，等于把口令丢了——需要认证的正常代理会被
	// 误判成不通，而这正是这批结论里最常见的一类假阴性。
	_, httpClient, err := netx.DialProxy(p, netx.DialOpts{Timeout: timeout, TLSInsecureSkipVerify: req.SkipTLSVerify})
	if err != nil {
		res.Error = redactSecret(err.Error(), req.Pass)
		return res, nil
	}

	// client 的 Timeout 由 DialProxy 按 2 倍预算给，这里再用 ctx 压一层硬顶：
	// 出口源是多级降级列表，不封顶的话"每条源都试到底"会远超用户设的值。
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	res.Probes = s.probeAllEgress(cctx, httpClient, req.Pass)
	res.LatencyMs = time.Since(start).Milliseconds()

	ip, source, agree, total := pickEgress(res.Probes)
	res.OK = ip != ""
	res.EgressIP = ip
	res.EgressSource = source
	res.EgressAgree = agree
	res.EgressSources = total
	if !res.OK {
		res.Error = redactSecret(probesSummary(res.Probes), req.Pass)
	}
	return res, nil
}

// pickEgress 从逐源结果里挑**多数一致**的出口 IP，并回报"几个源给了同一个值"。
//
// 为什么不能取第一个成功的源：机主真机实测过一次——同一台 SOCKS5 下 ifconfig.me
// 报 117.157.x，而 icanhazip / 3322 / oray 三个源一致报 36.142.x（代理端 NAT 池
// 多出口，各源落点线路不同）。按"第一个回来的"取值会把少数派当结论对外报出去，
// 用户自然怀疑工具测错了。众数更贴近真相；而把 agree/total 一并给出，少数派情况
// 用户自己看得见，不用猜。仍不拿"出口 IP 变没变"做通断判据（多出口本就正常）。
func pickEgress(probes []EgressProbe) (ip, source string, agree, total int) {
	counts := make(map[string]int)
	origin := make(map[string]string)
	order := make([]string, 0, len(probes))
	for _, p := range probes {
		if p.IP == "" {
			continue
		}
		if _, seen := counts[p.IP]; !seen {
			order = append(order, p.IP)
			origin[p.IP] = p.Source
		}
		counts[p.IP]++
		total++
	}
	for _, cand := range order {
		if counts[cand] > agree {
			ip, agree, source = cand, counts[cand], origin[cand]
		}
	}
	return ip, source, agree, total
}

// probeAllEgress 并发逐源各查一次，**每源结果都留下**（不只取判通那一发）。
// 各源共用同一条 ctx，也就是用户设的单条总时限：并发不叠加耗时，而任一源拖到超时
// 就如实记超时，不给它续命。
//
// 只用 wg.Go，不另写 wg.Add——WaitGroup.Go 自带计数，两处都写会让 Wait 永远等不到
// 归零（这个坑记在 docs/TROUBLESHOOTING.md #109）。
func (s *Scanner) probeAllEgress(ctx context.Context, client *http.Client, secret string) []EgressProbe {
	probes := make([]EgressProbe, len(proxyCheckEgressProviders))
	var wg sync.WaitGroup
	for i, source := range proxyCheckEgressProviders {
		wg.Go(func() {
			probe := EgressProbe{Source: source}
			start := time.Now()
			// 单源列表喂给 QueryEgress：它内部就不降级了，这一发只测这一个源
			ip, used, err := netx.QueryEgress(ctx, client, []string{source})
			probe.LatencyMs = time.Since(start).Milliseconds()
			if err != nil {
				probe.Error = redactSecret(shortGoError(err.Error()), secret)
			} else {
				probe.IP = ip
				if used != "" {
					probe.Source = used
				}
			}
			probes[i] = probe
		})
	}
	wg.Wait()
	return probes
}

// goErrNoise 剥掉 Go 网络错误的套话前缀。源名已经在每一行结果里给出，
// `探测出网IP失败: Get "https://…": ` 这类包装只是把同一件事重复一遍，留着它
// 用户要读三行才看到真正的错误（`socks connect … host unreachable`）。
var goErrNoise = regexp.MustCompile(`^(?:探测出网IP失败:\s*)?(?:Get|Head|Post)\s+"[^"]*":\s*`)

func shortGoError(msg string) string {
	if trimmed := strings.TrimSpace(goErrNoise.ReplaceAllString(msg, "")); trimmed != "" {
		return trimmed
	}
	return msg
}

// probesSummary 汇成一句失败原因。明细已在 Probes 里并排显示，这里不重抄一遍，
// 只给出"几个源都没拿到"加上第一个具体原因。
func probesSummary(probes []EgressProbe) string {
	first, failed := "", 0
	for _, p := range probes {
		if p.Error != "" {
			failed++
			if first == "" {
				first = p.Error
			}
		}
	}
	if first == "" {
		return "未能获取出口 IP"
	}
	if failed > 1 {
		return fmt.Sprintf("%d 个源都没拿到出口 IP；首个原因：%s", failed, first)
	}
	return first
}

// canonicalAddr 把结构化字段拼成一条代理地址。凭据在这里做百分号编码——
// 密码里出现 @ : / 时裸拼会让后端把用户名切错、把端口读成密码的一部分。
func (r ProxyCheckRequest) canonicalAddr() (string, error) {
	kind := strings.ToLower(strings.TrimSpace(r.Kind))
	if kind == "" {
		return "", errors.New("请选择代理协议")
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", errors.New("请填写代理地址（IP 或主机名）")
	}
	// 分栏表单最常见的误用：把整条 URL 塞进地址栏，静默拼出坏地址比报错更糟。
	// 判"塞了端口"不能用"含冒号"——IPv6 地址全是冒号，那样会把 ::1 一并拒掉；
	// 真塞了端口的形态是带 `]`，或是"含冒号又解析不成 IP"。
	if strings.Contains(host, "://") || strings.Contains(host, "@") {
		return "", errors.New("地址栏只填 IP 或主机名——协议用下拉，端口与账号密码有独立栏")
	}
	// 方括号要分两种情形：`[::1]` 是 IPv6 的合法书写（从 `ssh user@[::1]` 抄来），
	// 而 `[::1]:1080` 才是真把端口写进了主机栏——判据是 ']' 之后还有没有内容。
	if i := strings.Index(host, "]"); i >= 0 && i != len(host)-1 {
		return "", fmt.Errorf("地址栏不要带端口（当前为 %q）——端口在右侧独立栏", host)
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", errors.New("地址栏只填 IP 或主机名——协议用下拉，端口与账号密码有独立栏")
	}
	if r.Port < 1 || r.Port > 65535 {
		return "", fmt.Errorf("端口需为 1~65535 的整数（当前 %d）", r.Port)
	}

	// URL 里的 IPv6 必须带方括号（`socks5://[::1]:1080`），否则 url.Parse 会把
	// 最后一段冒号内容当成端口，地址直接错位。
	hostURL := host
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		hostURL = "[" + host + "]"
	}

	cred := ""
	if user := strings.TrimSpace(r.User); user != "" {
		cred = url.UserPassword(user, r.Pass).String() + "@" // 自带百分号编码
	}
	return fmt.Sprintf("%s://%s%s:%d", kind, cred, hostURL, r.Port), nil
}

func (r ProxyCheckRequest) effectiveTimeoutMs() int {
	ms := r.TimeoutMs
	switch {
	case ms <= 0:
		return ProxyCheckDefaultTimeoutMs
	case ms < ProxyCheckMinTimeoutMs:
		return ProxyCheckMinTimeoutMs
	case ms > ProxyCheckMaxTimeoutMs:
		return ProxyCheckMaxTimeoutMs
	}
	return ms
}

// redactSecret 把错误文本里的凭据形态抹掉。
//
// 为什么必须有这一步：Go 的 url.Error 会把**原始 URL 整条**写进错误文本，而
// netx.ParseProxy 失败正是这条路径——格式写错的地址（往往就带着密码）会顺着
// RPC error → 前端横幅 → 日志一路出去。拨号失败的 error 同理内嵌地址。
// logging.Redact 只打码 `password=` 这类赋值式写法，盖不住 URL userinfo 形态，
// 所以在此就地抹净，不指望日志层兜底。三种形态都试：原值、两种转义写法。
func redactSecret(msg, secret string) string {
	if secret == "" {
		return msg
	}
	for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
		if form != "" {
			msg = strings.ReplaceAll(msg, form, "***")
		}
	}
	return msg
}
