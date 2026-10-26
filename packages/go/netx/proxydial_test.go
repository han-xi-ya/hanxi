package netx

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件锁的是"显式代理拨号链"的行为面。portscan 的代理中继与 proxycheck 的代理
// 体检共用这一份实现，因此下面几条**不是可优化项，而是回归防线**：
//   - 凭据在类型层就漏不出去（String/JSON 双堵）；
//   - HTTP(S) 代理分支的 dialer 恒为直连（改了会静默变 portscan 的扫描语义）；
//   - 出口核验不跟跳转、响应体封顶（对来路不明的代理必须如此）。

func TestParseProxy(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantKind    ProxyKind
		wantHost    string
		wantCanon   string
		wantErrText string // 非空则期望报错且错误信息含此子串
	}{
		{name: "空串即直连", in: "", wantKind: ProxyDirect, wantCanon: "direct"},
		{name: "裸 host:port 容错为 socks5", in: "127.0.0.1:7890", wantKind: ProxySOCKS5, wantHost: "127.0.0.1:7890", wantCanon: "socks5://127.0.0.1:7890"},
		{name: "带空格仍解析", in: "  socks5://10.0.0.1:1080  ", wantKind: ProxySOCKS5, wantHost: "10.0.0.1:1080", wantCanon: "socks5://10.0.0.1:1080"},
		{name: "socks5h 与 socks5 同义并归一化", in: "socks5h://a.example:1080", wantKind: ProxySOCKS5, wantCanon: "socks5://a.example:1080"},
		{name: "http 代理", in: "http://127.0.0.1:7890", wantKind: ProxyHTTP, wantCanon: "http://127.0.0.1:7890"},
		{name: "https 代理", in: "https://proxy.example:8443", wantKind: ProxyHTTPS, wantCanon: "https://proxy.example:8443"},
		{name: "大写法归一", in: "SOCKS5://Host:1080", wantKind: ProxySOCKS5, wantCanon: "socks5://Host:1080"},
		{name: "IPv6 带端口", in: "socks5://[::1]:7890", wantKind: ProxySOCKS5, wantHost: "[::1]:7890", wantCanon: "socks5://[::1]:7890"},
		// 凭据形态单列在下面 TestParseProxyCredential 里验剥密，这里只锁 Kind/Host。
		{name: "缺端口必须拒", in: "127.0.0.1", wantErrText: "host:port"},
		{name: "socks4 明确拒并给理由", in: "socks4://127.0.0.1:1080", wantErrText: "SOCKS4"},
		{name: "未知协议如实报不支持", in: "ss://127.0.0.1:8388", wantErrText: "不支持的代理协议"},
		{name: "缺主机", in: "socks5://:1080", wantErrText: "缺少主机"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseProxy(c.in)
			if c.wantErrText != "" {
				if err == nil {
					t.Fatalf("ParseProxy(%q) 应报错，实际得 %+v", c.in, p)
				}
				if !strings.Contains(err.Error(), c.wantErrText) {
					t.Fatalf("ParseProxy(%q) 错误信息应含 %q，实际：%v", c.in, c.wantErrText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProxy(%q) 意外报错: %v", c.in, err)
			}
			if p.Kind != c.wantKind {
				t.Errorf("ParseProxy(%q).Kind = %q, want %q", c.in, p.Kind, c.wantKind)
			}
			if c.wantHost != "" && p.Host != c.wantHost {
				t.Errorf("ParseProxy(%q).Host = %q, want %q", c.in, p.Host, c.wantHost)
			}
			if c.wantCanon != "" && p.Canonical != c.wantCanon {
				t.Errorf("ParseProxy(%q).Canonical = %q, want %q", c.in, p.Canonical, c.wantCanon)
			}
		})
	}
}

// TestCredentialNeverLeaks 是"口令不落盘"这条产品口径的**结构性**防线：
// 三条最易泄露的路（字符串拼接、JSON 序列化、错误文本）都必须是剥凭据形。
// 靠调用方"记得剥"是约定，靠类型不导出才是防不住的那种防。
func TestCredentialNeverLeaks(t *testing.T) {
	const secret = "Sup3r-S3cret!"
	in := "socks5://alice:" + secret + "@proxy.example.com:1080"

	p, err := ParseProxy(in)
	if err != nil {
		t.Fatalf("带凭据地址应解析成功: %v", err)
	}
	if !p.HasCredentials() {
		t.Fatal("应识别出已带凭据")
	}
	user, pass, ok := p.Credentials()
	if !ok || user != "alice" || pass != secret {
		t.Fatalf("Credentials() 取用失败: %q %q %v", user, pass, ok)
	}

	// 路 1：字符串拼接（fmt 的 %v/%s、日志里手写、error 包装都走这里）
	for _, form := range []string{p.String(), fmt.Sprint(p), fmt.Sprintf("%v", p), fmt.Sprintf("%s", p)} {
		if strings.Contains(form, secret) || strings.Contains(form, "alice") {
			t.Fatalf("字符串形态泄露凭据: %q", form)
		}
	}
	if got := fmt.Sprintf("代理 %v 连接失败", p); strings.Contains(got, secret) {
		t.Fatalf("error 文本泄露凭据: %q", got)
	}

	// 路 2：JSON（配置落盘、事件体）
	for _, blob := range []string{marshal(t, p), marshal(t, []ParsedProxyRef{refOf(p)})} {
		if strings.Contains(blob, secret) {
			t.Fatalf("JSON 泄露凭据: %s", blob)
		}
	}

	// HTTP 代理同理：CONNECT 认证要用凭据，但它藏在非导出 raw 里，外面拿不到
	h, err := ParseProxy("http://bob:" + secret + "@127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	if s := h.String(); strings.Contains(s, secret) {
		t.Fatalf("HTTP 代理 String() 泄露凭据: %q", s)
	}
}

// 间接包一层，专门测"把结构体塞进 JSON"这条路。
type ParsedProxyRef struct {
	Addr *ParsedProxy `json:"addr"`
}

func refOf(p *ParsedProxy) ParsedProxyRef { return ParsedProxyRef{Addr: p} }

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(b)
}

func TestDialProxyNilMeansDirect(t *testing.T) {
	l := newEchoListener(t)
	defer l.Close()

	dialer, client, err := DialProxy(nil, DialOpts{Timeout: time.Second})
	if err != nil {
		t.Fatalf("nil 代理应直连可用: %v", err)
	}
	assertDirectDial(t, dialer, l)
	assertClientReaches(t, client, l)
}

// TestHTTPProxyDialerIsStillDirect 锁死最容易被"顺手修正"的一条既有语义：
// HTTP(S) 代理只作用于返回的 http.Client（CONNECT 隧道），返回的 dialer
// **仍是直连拨号器**。portscan 的裸 TCP 探测走 dialer，所以用户配 HTTP 代理时
// 端口探测探的是直达靶——这是 PRD「SOCKS5/HTTP 代理扫描」那行的真实含义。
// 若哪天有人把 dialer 改成过代理，本用例的两条断言会立刻分叉。
func TestHTTPProxyDialerIsStillDirect(t *testing.T) {
	l := newEchoListener(t)
	defer l.Close()

	// 一个确定没人监听的代理地址（先占位再关掉，确保 refused）
	deadProxy := takeThenReleaseAddr(t)

	p, err := ParseProxy("http://" + deadProxy)
	if err != nil {
		t.Fatal(err)
	}
	dialer, client, err := DialProxy(p, DialOpts{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// ① dialer 直达靶成功（根本没碰那个不存在的代理）
	assertDirectDial(t, dialer, l)

	// ② 同一个地址经 http.Client 则失败——证明代理只作用于 client 一侧
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := QueryEgress(ctx, client, []string{"http://" + l.Addr().String()}); err == nil {
		t.Fatal("经 HTTP 代理的请求应因代理不可达而失败，说明 client 没走代理")
	}
}

func TestDialProxyRespectsTLSVerifyOption(t *testing.T) {
	// 两个方向都锁：portscan 恒传 true（扫描不问证书），proxycheck 默认 false
	//（证书被中间网关替换正是它要报的结论）。谁把这里"统一"掉，这条就红。
	for _, insecure := range []bool{true, false} {
		p, err := ParseProxy("127.0.0.1:7890")
		if err != nil {
			t.Fatal(err)
		}
		_, client, err := DialProxy(p, DialOpts{Timeout: time.Second, TLSInsecureSkipVerify: insecure})
		if err != nil {
			t.Fatal(err)
		}
		tr := transportOf(t, client)
		if tr.TLSClientConfig == nil {
			t.Fatal("应显式带 TLSClientConfig（与上收前的形状一致）")
		}
		if tr.TLSClientConfig.InsecureSkipVerify != insecure {
			t.Fatalf("TLSInsecureSkipVerify=%v 未透传，实得 %v", insecure, tr.TLSClientConfig.InsecureSkipVerify)
		}
	}
}

func TestDialProxyTransportShapeUnchanged(t *testing.T) {
	// 上收前 portscan 的 Transport 形状（禁用长连接、单次探测用）必须逐字保住。
	for _, in := range []string{"", "127.0.0.1:1080", "http://127.0.0.1:7890"} {
		p, err := ParseProxy(in)
		if err != nil {
			t.Fatalf("ParseProxy(%q): %v", in, err)
		}
		_, client, err := DialProxy(p, DialOpts{Timeout: 800 * time.Millisecond})
		if err != nil {
			t.Fatalf("DialProxy(%q): %v", in, err)
		}
		tr := transportOf(t, client)
		if !tr.DisableKeepAlives {
			t.Errorf("%q: DisableKeepAlives 应保持 true", in)
		}
		if tr.MaxIdleConns != 0 {
			t.Errorf("%q: MaxIdleConns 应保持 0", in)
		}
		if tr.ResponseHeaderTimeout != 800*time.Millisecond {
			t.Errorf("%q: ResponseHeaderTimeout = %v", in, tr.ResponseHeaderTimeout)
		}
		if want := 2 * 800 * time.Millisecond; client.Timeout != want {
			t.Errorf("%q: client.Timeout = %v, want %v（既有 2 倍口径）", in, client.Timeout, want)
		}
	}
}

func TestQueryEgress(t *testing.T) {
	cases := []struct {
		name     string
		handler  http.Handler
		wantIP   string
		wantErr  bool
		skipDegr bool // true 则只挂这一个源
	}{
		{name: "纯文本 IP", handler: textHandler("203.0.113.9\n"), wantIP: "203.0.113.9"},
		{name: "带杂质文本抠 IPv4", handler: textHandler("Your IP is 198.51.100.23 thanks\n"), wantIP: "198.51.100.23"},
		{name: "IPv6 直答", handler: textHandler("2001:db8::42"), wantIP: "2001:db8::42"},
		{name: "非 200 不算成功", handler: statusHandler(http.StatusForbidden, "203.0.113.9"), wantErr: true, skipDegr: true},
		{name: "跳转绝不跟随", handler: redirectHandler(), wantErr: true, skipDegr: true},
		{name: "胡言乱语不算 IP", handler: textHandler("<html>no ip here</html>"), wantErr: true, skipDegr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()

			providers := []string{srv.URL}
			if !c.skipDegr {
				providers = append(providers, "http://127.0.0.1:1/decoy") // 降级位，正常用不到
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			ip, source, err := QueryEgress(ctx, DirectClient(2*time.Second), providers)
			if c.wantErr {
				if err == nil {
					t.Fatalf("应失败，实得 ip=%q source=%q", ip, source)
				}
				return
			}
			if err != nil {
				t.Fatalf("应成功: %v", err)
			}
			if ip != c.wantIP {
				t.Errorf("ip = %q, want %q", ip, c.wantIP)
			}
			if source != srv.URL {
				t.Errorf("source 必须是命中那个源（定性权在上层），实得 %q", source)
			}
		})
	}
}

// TestQueryEgressDegradesToNextSource 验多源降级确实生效（首个源坏→第二个）。
func TestQueryEgressDegradesToNextSource(t *testing.T) {
	bad := httptest.NewServer(statusHandler(http.StatusInternalServerError, "boom"))
	defer bad.Close()
	good := httptest.NewServer(textHandler("192.0.2.66"))
	defer good.Close()

	ip, source, err := QueryEgress(context.Background(), DirectClient(2*time.Second), []string{bad.URL, good.URL})
	if err != nil {
		t.Fatalf("应降级到第二个源: %v", err)
	}
	if ip != "192.0.2.66" || source != good.URL {
		t.Fatalf("降级结果不对: ip=%q source=%q", ip, source)
	}
}

// TestQueryEgressIgnoresRedirect 单独把"不跟跳转"拎出来讲清它防的是什么：
// 出口核验的请求走的是**待测的不可信代理**，它能 302 把我们送到一个自报家门的
// 假 checkip 页——跟跳转等于把"出口是谁"这个结论交给对方决定。
func TestQueryEgressIgnoresRedirect(t *testing.T) {
	var followed int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed++
		fmt.Fprint(w, "192.0.2.99")
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	_, _, err := QueryEgress(context.Background(), DirectClient(2*time.Second), []string{origin.URL})
	if err == nil {
		t.Fatal("302 必须如实暴露为失败，不能跟跳转拿答案")
	}
	if followed != 0 {
		t.Fatalf("跳转被跟随了 %d 次（出口核验应恒定不跟）", followed)
	}
}

// TestQueryEgressCapsBody 给一个远超上限的响应体：必须在封顶处收住并照常降级，
// 而不是把对端的流体读进内存（上收前的 bufio.ReadString 无上限，这是补的护栏）。
func TestQueryEgressCapsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 无 IP 可抠的巨量杂质：截断后应当"解析不出"从而报错，而非 OOM 或长挂。
		w.Header().Set("Content-Type", "text/plain")
		buf := make([]byte, 4096)
		for i := range buf {
			buf[i] = 'x'
		}
		for i := 0; i < 4000; i++ { // ~16MB
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if _, _, err := QueryEgress(ctx, DirectClient(4*time.Second), []string{srv.URL}); err == nil {
		t.Fatal("巨量杂质里抠不出 IP，应报错")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("耗时 %v，疑似未封顶读取", elapsed)
	}
}

func TestQueryEgressGuards(t *testing.T) {
	if _, _, err := QueryEgress(context.Background(), nil, nil); err == nil {
		t.Error("nil client 应直接报错，不能静默返回空 IP")
	}
	// 单源不可达即报"探测出网IP失败"（错误文本保持上收前口径；providers 传空
	// 回落内置列表的分支不在单测真打外网，此处不驱动）
	srv := httptest.NewServer(statusHandler(http.StatusTeapot, ""))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := QueryEgress(ctx, DirectClient(time.Second), []string{srv.URL}); !strings.Contains(err.Error(), "探测出网IP失败") {
		t.Errorf("错误文本应保持上收前的口径，实得: %v", err)
	}
}

// ---------- 测试辅助 ----------

func newEchoListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return l
}

// takeThenReleaseAddr 占一个临时端口再释放，确保后续连接必被 refused。
func takeThenReleaseAddr(t *testing.T) string {
	t.Helper()
	l := newEchoListener(t)
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func assertDirectDial(t *testing.T, dialer ContextDialer, l net.Listener) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dialer 直连本地监听应成功: %v", err)
	}
	_ = conn.Close()
}

func assertClientReaches(t *testing.T, client *http.Client, l net.Listener) {
	t.Helper()
	srv := &http.Server{Handler: textHandler("192.0.2.1")}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	_ = l // 仅为语义清晰：本辅助不依赖调用方的 listener

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("直连 client 请求应成功: %v", err)
	}
	_ = resp.Body.Close()
}

func transportOf(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("Transport 应为 *http.Transport，实得 %T", client.Transport)
	}
	return tr
}

func textHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	})
}

func statusHandler(code int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	})
}

func redirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/never", http.StatusFound)
	})
}
