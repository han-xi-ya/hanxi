package netx

// 本文件是**显式代理拨号**链：调用方给一条代理地址，这里解析并建出过该代理的
// Dialer 与 http.Client，用于"明知要用代理"的场景（portscan 的代理中继扫描、
// proxycheck 的代理连通性体检）。
//
// 与 client.go 的**系统代理链**（环境变量 → WinINET → 直连，语义是"跟随浏览器"）
// 刻意分成两条，勿互相拿错：
//   - 系统代理链不接 `socks=` 项——net/http 原生 Transport 不认 SOCKS，链里做半
//     成品只会让用户以为"跟随了浏览器"（该纪律由 client_test.go 锁死，未松动）；
//   - 显式拨号链相反**专管 SOCKS**，因为它自己拨号，不经 Transport 的 Proxy 钩子。

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ContextDialer 统一标准 net.Dialer 与 x/net SOCKS 拨号器的最小接口
// （原 portscan.ContextDialer 上收，portscan 侧改类型别名以保持包内引用不变）。
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// ProxyKind 显式代理的方言类别。
type ProxyKind string

const (
	ProxyDirect ProxyKind = "direct"
	ProxySOCKS5 ProxyKind = "socks5"
	ProxyHTTP   ProxyKind = "http"
	ProxyHTTPS  ProxyKind = "https"
)

// credential 承载代理用户名/密码。
//
// **结构性防泄露**（不是约定，是类型设计）：本字段非导出，且 ParsedProxy 的
// String()/MarshalJSON 恒输出剥凭据后的 Canonical——于是 slog（logging.Redact
// 只做赋值式打码，**盖不住 URL userinfo 形态**）、事件载荷、配置落盘三条路都
// 物理拿不到口令。要取用必须显式调 Credentials()，调用点自带提醒。
type credential struct {
	user string
	pass string
}

// ParsedProxy 是一条已解析的显式代理地址。零值不可用，须经 ParseProxy。
type ParsedProxy struct {
	// Kind 是方言类别；Direct 表示"不走代理"。
	Kind ProxyKind
	// Host 是 host:port，**恒不含凭据**。
	Host string
	// Canonical 是剥凭据的规范形，展示/序列化/落盘一律用它。
	Canonical string

	// raw 含 userinfo：HTTP(S) 代理的 Proxy-Authorization 要靠标准库从它自动注入，
	// 所以必须留着，但只在本包内可见。
	raw *url.URL
	// auth 是 SOCKS5 用户名密码认证所需凭据（无凭据为 nil）。
	auth *credential
}

// String 恒出剥凭据形——这是本类型的默认展示姿势，任何日志/拼接走它。
func (p *ParsedProxy) String() string {
	if p == nil {
		return string(ProxyDirect)
	}
	return p.Canonical
}

// MarshalJSON 同样只序列化 Canonical：把 ParsedProxy 整个塞进 JSON（配置落盘、
// 事件体）也漏不出密码。走 encoding/json 而非手拼引号——地址里出现引号时手拼会
// 产出非法 JSON，那是"看起来聪明但不安全"的写法。
func (p *ParsedProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// IsDirect 报告是否为"不经代理"的直连。
func (p *ParsedProxy) IsDirect() bool { return p == nil || p.Kind == ProxyDirect }

// HasCredentials 报告解析时是否带来了凭据（**不落盘**，只用于展示"该条已带认证"）。
func (p *ParsedProxy) HasCredentials() bool { return p != nil && p.auth != nil }

// Credentials 显式取凭据。调用方须自行保证：不进配置存储、不进日志、不进事件体。
func (p *ParsedProxy) Credentials() (user, pass string, ok bool) {
	if p == nil || p.auth == nil {
		return "", "", false
	}
	return p.auth.user, p.auth.pass, true
}

// ParseProxy 解析显式代理地址。支持的形态：
//
//	""                        → 直连
//	socks5://[u:p@]host:port  → SOCKS5
//	127.0.0.1:7890            → 裸 host:port 容错为 SOCKS5（自 portscan 上收的既有口径）
//	http[s]://[u:p@]host:port → HTTP(S) 代理（https 靶靠 CONNECT 隧道）
//
// 诚实边界：
//   - `socks5h` 与 `socks5` **行为完全相同**——x/net 对非 IP 目标一律按 ATYPE
//     域名写进 CONNECT（internal/socks/client.go 的 AddrTypeFQDN 分支），目标域名
//     恒由**代理端**解析，不存在 curl 那对 `-5`/`-5h` 的本地/远端解析之别。
//     两写法归一化为 socks5，UI 与文档不得暗示二者有别。
//   - 拒绝 socks4/socks4a：该代无用户名密码认证，收下只会给出误导性结论。
func ParseProxy(raw string) (*ParsedProxy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &ParsedProxy{Kind: ProxyDirect, Canonical: string(ProxyDirect)}, nil
	}
	if !strings.Contains(raw, "://") {
		raw = string(ProxySOCKS5) + "://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("代理地址格式无效: %w", err)
	}

	host := strings.TrimSpace(u.Host)
	if host == "" {
		return nil, errors.New("代理地址缺少主机与端口")
	}
	hostname, port, err := net.SplitHostPort(host)
	switch {
	case err != nil:
		return nil, fmt.Errorf("代理地址需写成 host:port（当前为 %q）", host)
	case strings.TrimSpace(hostname) == "":
		return nil, fmt.Errorf("代理地址缺少主机名（当前为 %q）", host)
	case strings.TrimSpace(port) == "":
		return nil, fmt.Errorf("代理地址缺少端口（当前为 %q）", host)
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "socks4", "socks4a":
		return nil, fmt.Errorf("不支持的代理协议: %s（SOCKS4 无用户名密码认证，请改用 socks5）", scheme)
	case "socks5", "socks5h":
		scheme = string(ProxySOCKS5)
	case string(ProxyHTTP), string(ProxyHTTPS):
	default:
		return nil, fmt.Errorf("不支持的代理协议: %s (仅支持 socks5 / http)", u.Scheme)
	}

	p := &ParsedProxy{
		Kind:      ProxyKind(scheme),
		Host:      host,
		Canonical: scheme + "://" + host, // 刻意不带 userinfo
		raw:       u,
	}
	if u.User != nil {
		c := &credential{user: u.User.Username()}
		if pass, ok := u.User.Password(); ok {
			c.pass = pass
		}
		p.auth = c
	}
	return p, nil
}

// DialOpts 是显式拨号的可选项。
type DialOpts struct {
	// Timeout 同时用作连接超时与响应头超时；http.Client 总超时按既有口径取 2 倍。
	Timeout time.Duration
	// TLSInsecureSkipVerify 决定是否跳过证书校验。
	//
	// 两条链的默认值**相反**，且各自都有正当理由，勿互相"借鉴"：
	//   - portscan 传 true——扫描只问"靶活着没"，证书链不参与存活判定；
	//   - proxycheck 传 false——代理背后是否有透明网关替换证书，本身就是它要报的结论。
	TLSInsecureSkipVerify bool
}

// DialProxy 按已解析的代理建出（ContextDialer, *http.Client）。
//
// 返回的 dialer 语义有一条**必须知道的既有行为**（不是 bug，勿"顺手修正"）：
// **HTTP(S) 代理分支返回的是直连 dialer**，因为 net/http 的 CONNECT 隧道只作用于
// 经该 Client 发出的请求，而 portscan 的裸 TCP 探测（probePort 里 dialer.DialContext
// 那一发）走的是 dialer。把它"修成"过代理会静默改变扫描结果语义——用户配 HTTP
// 代理时端口探测探的就是直达靶，PRD 里"SOCKS5/HTTP 代理扫描"那行的含义也随它变。
// 需要"过 HTTP 代理的隧道能力"请用返回的 http.Client，不要用返回的 dialer。
func DialProxy(p *ParsedProxy, o DialOpts) (ContextDialer, *http.Client, error) {
	baseDialer := &net.Dialer{Timeout: o.Timeout}

	var dialer ContextDialer = baseDialer
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: o.TLSInsecureSkipVerify},
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
		ResponseHeaderTimeout: o.Timeout,
		DialContext:           baseDialer.DialContext,
	}

	switch {
	case p == nil || p.IsDirect():
		// 恒定直连（N25 豁免登记，原样带上收）：扫描器/探测器的 HTTP 探测语义是
		// "从本机直达目标"，套用户系统代理既无意义（代理看不见 10.x/192.168.x 内网
		// 靶）还会伪造存活结论；用户显式配置的 socks5/http 代理走下面分支，属主动
		// 功能而非系统链。此排除为永久口径，后续公共件改造勿误伤。

	case p.Kind == ProxySOCKS5:
		var auth *proxy.Auth
		if p.auth != nil {
			auth = &proxy.Auth{User: p.auth.user, Password: p.auth.pass}
		}
		// forward dialer 必须包一层 deadline：x/net 拿到这条 conn 后是**同步**读
		// SOCKS greeting 的，期间不看 ctx——对端若 accept 但不回话（端口开着却是个
		// HTTP 服务、透明转发盒、captive portal），握手会永久阻塞在 io.ReadFull 上，
		// http.Client 的 Timeout 与 ctx 都管不着它（Transport 只是放弃了 dial 结果，
		// goroutine 与连接仍悬挂）。而 proxy.SOCKS5 用的正是我们给的这条 conn，
		// 所以在交付前设死线是唯一能给它封顶的位置。
		// 取 2 倍而非 1 倍：这条 conn 握手成功后会被复用为隧道跑真实请求，
		// 死线不能比 client.Timeout（同为 2×Timeout）更短，否则正常慢代理会被误判。
		socksDialer, err := proxy.SOCKS5("tcp", p.Host, auth, deadlineDialer{
			inner:  baseDialer,
			budget: o.Timeout * 2,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
		}
		// x/net 的 *internal/socks.Dialer 本身实现 DialContext，断言恒成立；
		// 这里不再备"不支持 context"的兜底适配器（那是历史死代码，见踩坑记录），
		// 一旦依赖换代使断言失败，如实报错比退回非取消式拨号安全。
		cd, ok := socksDialer.(ContextDialer)
		if !ok {
			return nil, nil, errors.New("x/net 的 SOCKS5 拨号器不支持 DialContext（依赖版本异常，请核对 golang.org/x/net）")
		}
		dialer = cd
		transport.DialContext = cd.DialContext // SOCKS 流量经隧道

	case p.Kind == ProxyHTTP || p.Kind == ProxyHTTPS:
		// 凭据留在 p.raw 的 userinfo 上：标准库会据此自动发 Proxy-Authorization
		// （transport.go 的 cm.proxyAuth 路径），不必手工注入请求头。
		transport.Proxy = http.ProxyURL(p.raw)
		// dialer 保持 baseDialer（直连）——见函数注释里那条既有行为说明。
	}

	return dialer, &http.Client{Transport: transport, Timeout: o.Timeout * 2}, nil
}

// deadlineDialer 给交给 x/net 的那条 conn 补上死线（缘由见 DialProxy 的 SOCKS 分支）。
// proxy.SOCKS5 的 forward 参数同时认 Dialer 与 ContextDialer，两个都实现一遍，
// 免得依赖哪天改走非 context 路径就把封顶丢了。
type deadlineDialer struct {
	inner  ContextDialer
	budget time.Duration
}

func (d deadlineDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.inner.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	// 死线**每次拨号各算一次**（存的是时长而非时刻）：同一个 client 会被用来做多源
	// 降级重试，若在此刻固定死时间，第二次重试就会拿到一条已过期的 conn。
	// 设不上死线也不把这条 conn 交出去：宁可当场失败，也不要一条可能永久悬挂的连接。
	if serr := conn.SetDeadline(time.Now().Add(d.budget)); serr != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("无法为代理连接设置超时: %w", serr)
	}
	return conn, nil
}

// Dial 满足 proxy.Dialer 的非 context 路径。
func (d deadlineDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

// egressBodyLimit 与 publicip 同口径（service.go 的 io.LimitReader(…, 512)）：
// 出口核验端点返回的是纯文本 IP，512 字节足够；对**来路不明的候选代理**必须封顶，
// 否则它可以拿一个无限流体喂给我们的读取缓冲（原 portscan 版用的是无上限
// bufio.ReadString('\n')，那是"只测自家可信代理"时代的写法）。
const egressBodyLimit = 512

// egressIPv4Re 从掺杂质的响应文本里抠 IPv4（供应商返回带前后缀的文本时兜底）。
// 提为包级：正则编译昂贵，不可在轮询循环内反复 MustCompile。
var egressIPv4Re = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)

// stopAtFirstResponse 拒绝跟随任何重定向。
//
// 为什么出口核验必须这样：请求走的是**待测的不可信代理**，它既能 MITM 也能把
// 我们 302 到自己的假 checkip 页；跟跳转等于把"出口是谁"这个结论交给对方决定，
// 而正确做法是让跳转以失败暴露出来。这些端点本就是纯文本 IP，任何跳转都不正常。
func stopAtFirstResponse(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// EgressProvidersV4 是出口 IPv4 核验的多源冗余列表（自 portscan 上收，顺序即降级顺序）。
//
// 如实登记一处未收口：publicip 另有一份**更宽**的列表（含 myip.ipip.net 等国内源与
// 独立的 IPv6 池）。本批不合并——合并会改 publicip 的实际查询顺序与它的既有测试面，
// 而这两件事与"代理体检"无关。后续若要收口成一处，需单独一批并同步 publicip 的用例。
var EgressProvidersV4 = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
	"https://ip.3322.net",
	"https://ddns.oray.com/checkip",
}

// QueryEgress 依次试各源，返回首个可信的出口 IP 与**它是哪个源给的**（source）。
//
// 调用方须自己判断结论强度：拿到与预期不同的 IP 只能说明"出口变了"，不能说明
// "代理是好的"；反之拿到相同 IP 也不能定性为假代理（同 NAT/CGNAT 池、ECMP 多出口、
// v4 与 v6 族不齐都会撞上）。故返回 source 而非 bool，把定性权留给上层。
//
// 传入的 client 会被浅拷贝一份以关掉跳转（共享其 Transport，不改调用方对象）。
func QueryEgress(ctx context.Context, client *http.Client, providers []string) (ip, source string, err error) {
	if client == nil {
		return "", "", errors.New("出口核验需要 http.Client")
	}
	c := *client // 浅拷贝：Transport 与连接池共享，只改重定向策略
	c.CheckRedirect = stopAtFirstResponse

	if len(providers) == 0 {
		providers = EgressProvidersV4
	}

	var lastErr error
	for _, provider := range providers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "curl/8.0.0")

		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, egressBodyLimit))
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s 返回状态 %d", provider, resp.StatusCode)
			continue
		}

		text := strings.TrimSpace(string(body))
		if parsed := net.ParseIP(text); parsed != nil {
			return parsed.String(), provider, nil
		}
		// 备用：文本里抠 IPv4（响应掺杂质时）
		if match := egressIPv4Re.FindString(text); match != "" && net.ParseIP(match) != nil {
			return match, provider, nil
		}
		lastErr = fmt.Errorf("%s 的响应不含有效 IP", provider)
	}

	if lastErr != nil {
		return "", "", fmt.Errorf("探测出网IP失败: %w", lastErr)
	}
	return "", "", errors.New("未能获取到有效的公网IP")
}
