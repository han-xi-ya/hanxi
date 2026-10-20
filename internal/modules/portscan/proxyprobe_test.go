package portscan

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/netx"
)

// 代理连通性测试的用例面。整套不碰外网：端到端那一轮的出口源在测试里换成
// httptest 本地靶（CI 既不该依赖公网，也不该让"测一条代理"真往 api.ipify.org 发请求）。

func TestCheckProxyRejectsBadForm(t *testing.T) {
	cases := []struct {
		name    string
		req     ProxyCheckRequest
		wantErr string
	}{
		{
			name:    "地址栏塞了整条 URL",
			req:     ProxyCheckRequest{Kind: "socks5", Host: "socks5://127.0.0.1:7890", Port: 7890},
			wantErr: "只填 IP 或主机名",
		},
		{
			name:    "地址栏塞了账号",
			req:     ProxyCheckRequest{Kind: "socks5", Host: "u:p@127.0.0.1", Port: 7890},
			wantErr: "只填 IP 或主机名",
		},
		{
			// 端口写进地址栏是表单误用的第三种形态，静默拼出坏地址比拦住更糟
			name:    "地址栏塞了端口",
			req:     ProxyCheckRequest{Kind: "socks5", Host: "127.0.0.1:7890", Port: 1080},
			wantErr: "只填 IP 或主机名",
		},
		{
			name:    "没选协议",
			req:     ProxyCheckRequest{Host: "127.0.0.1", Port: 7890},
			wantErr: "请选择代理协议",
		},
		{
			name:    "端口越界",
			req:     ProxyCheckRequest{Kind: "socks5", Host: "127.0.0.1", Port: 70000},
			wantErr: "1~65535",
		},
		{
			name:    "端口为 0",
			req:     ProxyCheckRequest{Kind: "http", Host: "127.0.0.1"},
			wantErr: "1~65535",
		},
		{
			name:    "地址为空",
			req:     ProxyCheckRequest{Kind: "socks5", Port: 1080},
			wantErr: "请填写代理地址",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := NewScanner().CheckProxy(context.Background(), c.req)
			if err == nil {
				t.Fatalf("应拒绝，实得结果 %+v", res)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("错误应含 %q，实得 %v", c.wantErr, err)
			}
			if res != nil {
				t.Errorf("参数被拒时不该带结果: %+v", res)
			}
		})
	}
}

func TestEffectiveTimeoutClamped(t *testing.T) {
	// 三档都要钳：太短会把好代理测成不通，太长一次要等半天，两头都不该静默放过。
	cases := []struct{ in, want int }{
		{0, ProxyCheckDefaultTimeoutMs},
		{-5000, ProxyCheckDefaultTimeoutMs},
		{100, ProxyCheckMinTimeoutMs},
		{999999, ProxyCheckMaxTimeoutMs},
		{7000, 7000},
	}
	for _, c := range cases {
		got := ProxyCheckRequest{TimeoutMs: c.in}.effectiveTimeoutMs()
		if got != c.want {
			t.Errorf("TimeoutMs=%d 应折算成 %d，实得 %d", c.in, c.want, got)
		}
	}
}

// TestCheckProxyCarriesNoCredentials 是「口令不落盘、每次现输」这条口径的验收：
// 结果是唯一会回到前端并被复制的东西，序列化后必须查不到密码；连**错误文本**
// 也不能带（Go 的 url.Error 会把原始 URL 整条写进错误，而格式写错的地址往往就带着密码）。
func TestCheckProxyCarriesNoCredentials(t *testing.T) {
	const secret = "Sup3r-S3cret"
	useLocalEgress(t, "192.0.2.7")
	dead := closedAddr(t)
	host, portStr, err := net.SplitHostPort(dead)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	res, err := NewScanner().CheckProxy(context.Background(), ProxyCheckRequest{
		Kind: "socks5", Host: host, Port: port, User: "alice", Pass: secret, TimeoutMs: 1200,
	})
	if err != nil {
		t.Fatalf("连不上属于正常结论，不该走 error 通道: %v", err)
	}
	if res.OK {
		t.Error("端口已关闭，不该判通")
	}
	if res.Error == "" {
		t.Error("不通必须带原因文本，否则用户无从下手")
	}
	if strings.Contains(res.Addr, secret) || strings.Contains(res.Addr, "alice") {
		t.Errorf("Addr 应为剥凭据形，实得 %q", res.Addr)
	}
	if strings.Contains(res.Error, secret) {
		t.Errorf("错误文本泄露口令: %q", res.Error)
	}
	blob, mErr := json.Marshal(res)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(blob), secret) {
		t.Errorf("结果序列化泄露口令: %s", blob)
	}

	// 最危险的一条：地址写得不合法让 ParseProxy 抛错，而 Go 的 url.Error 会把
	// **原始 URL 整条**写进错误文本（里面就有密码）。这个 host 过得了字段级校验
	// （不含 :// @ 和冒号），却过不了 url.Parse 的百分号转义检查。
	_, bErr := NewScanner().CheckProxy(context.Background(), ProxyCheckRequest{
		Kind: "socks5", Host: "a%zz b", Port: port, User: "bob", Pass: secret, TimeoutMs: 1200,
	})
	if bErr == nil {
		t.Fatal("非法 host 形态应在解析处报错")
	}
	if strings.Contains(bErr.Error(), secret) {
		t.Errorf("解析错误内嵌了原始 URL，泄露口令: %v", bErr)
	}
}

// TestCheckProxyThroughLocalHTTPProxy 走完整链路：拼地址 → ParseProxy → DialProxy（过代理）
// → QueryEgress（打本地靶）→ 判通并拿到出口 IP。
// 判定口径是"能经它拿到出口 IP 才算通"——光 TCP 连上代理端口不算，那正是假阳性。
func TestCheckProxyThroughLocalHTTPProxy(t *testing.T) {
	const wantIP = "203.0.113.42"
	useLocalEgress(t, wantIP)
	proxyAddr := startForwardingProxy(t)
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	port, _ := strconv.Atoi(portStr)

	res, err := NewScanner().CheckProxy(context.Background(), ProxyCheckRequest{
		Kind: "http", Host: host, Port: port, TimeoutMs: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("经本地假代理应判通，实得 %+v", res)
	}
	if res.EgressIP != wantIP {
		t.Errorf("EgressIP = %q, want %q", res.EgressIP, wantIP)
	}
	if !strings.HasPrefix(res.Addr, "http://127.0.0.1:") {
		t.Errorf("Addr 应为剥凭据规范形，实得 %q", res.Addr)
	}
}

// TestCheckProxyPortOpenButNotAProxy 锁最常见的误判：端口开着，背后却是个普通
// HTTP 服务（透明转发盒、别的应用占了 7890）。必须判不通。
func TestCheckProxyPortOpenButNotAProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "I am a web server, not a proxy")
	}))
	defer srv.Close()
	useLocalEgress(t, "203.0.113.99") // 靶就挂在同一个 web 服务上，永远拿不到有效 IP

	addr := strings.TrimPrefix(srv.URL, "http://")
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	for _, kind := range []string{"http", "socks5"} {
		res, err := NewScanner().CheckProxy(context.Background(), ProxyCheckRequest{
			Kind: kind, Host: host, Port: port, TimeoutMs: 2000,
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if res.OK {
			t.Errorf("%s 指向普通 web 服务不该判通: %+v", kind, res)
		}
		if res.Error == "" {
			t.Errorf("%s 不通应带原因: %+v", kind, res)
		}
	}
}

// TestRedactSecret 直接锁打码函数：原值、转义值两种形态都要抹到。
func TestRedactSecret(t *testing.T) {
	got := redactSecret(`dial socks5://bob:p%40ss@1.1.1.1:1080: EOF`, "p@ss")
	if strings.Contains(got, "p%40ss") || strings.Contains(got, "p@ss") {
		t.Errorf("转义形态未抹净: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("应替换为掩码: %q", got)
	}
	if same := redactSecret("无凭据的错误", ""); same != "无凭据的错误" {
		t.Errorf("空凭据不该改动文本: %q", same)
	}
}

// ---------- 测试辅助 ----------

// useLocalEgress 把出口核验源换成一个本地 httptest 靶；t.Cleanup 复原全局。
func useLocalEgress(t *testing.T, ip string) {
	t.Helper()
	useSources(t, []string{egressServer(t, ip)})
}

// egressServer 起一个"返回纯文本 IP"的本地出口核验靶，返回它的请求 URL。
func egressServer(t *testing.T, ip string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s\n", ip)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/ip"
}

// useSources 覆盖逐源探测用的源列表；t.Cleanup 复原。
func useSources(t *testing.T, providers []string) {
	t.Helper()
	old := proxyCheckEgressProviders
	proxyCheckEgressProviders = providers
	t.Cleanup(func() { proxyCheckEgressProviders = old })
}

// TestCheckProxyShowsEverySource 锁"逐源并排显示"这件事：三个源里一个坏两个好，
// 结果里三条都得在，判通取第一个成功值，坏源的原因也得留着给用户看——只报
// "第一个成功"会让用户怀疑别的源是不是也测过（真机就撞上过：两源给出不同 IP）。
func TestCheckProxyShowsEverySource(t *testing.T) {
	sources := []string{
		egressServer(t, "198.51.100.1"),
		brokenEgressServer(t),
		egressServer(t, "198.51.100.2"),
	}
	useSources(t, sources)
	host, portStr, _ := net.SplitHostPort(startForwardingProxy(t))
	port, _ := strconv.Atoi(portStr)

	res, err := NewScanner().CheckProxy(context.Background(), ProxyCheckRequest{
		Kind: "http", Host: host, Port: port, TimeoutMs: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Probes) != 3 {
		t.Fatalf("三个源都该有结果，实得 %d：%+v", len(res.Probes), res.Probes)
	}
	if !res.OK || res.EgressIP != "198.51.100.1" {
		t.Errorf("判通应取第一个成功源，实得 ok=%v ip=%q", res.OK, res.EgressIP)
	}
	if res.EgressSource != sources[0] {
		t.Errorf("应标明判通那发出自哪个源，实得 %q want %q", res.EgressSource, sources[0])
	}
	failed := 0
	for _, p := range res.Probes {
		if p.Source == "" {
			t.Errorf("每条探测都要有源名，否则并排显示没有意义: %+v", p)
		}
		if p.Error != "" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("应恰有一个源留下失败原因，实得 %d：%+v", failed, res.Probes)
	}
}

// brokenEgressServer 一个只会 500 的源，用来造"部分源失败"的真实形态。
func brokenEgressServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/ip"
}

// closedAddr 取一个确定无人在听的地址（占位即释放），用来造「连不上」。
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startForwardingProxy 起一个最小 HTTP 代理：读绝对形式请求、转发、按 wire format 写回。
// 只支持明文 http 靶（绝对形式 GET），不支持 CONNECT——所以配套靶必须是 httptest
// 的 http 服务，别拿它去测 https 靶。
func startForwardingProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveForward(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func serveForward(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	httpReq, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	// 经代理读到的请求是绝对形式（URL 自带 scheme+host），可直接转发；
	// 但 RequestURI 在 client 侧请求里是非法字段，必须清掉否则 Do 直接报错。
	httpReq.RequestURI = ""

	resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(httpReq)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	// Response.Write 按 wire format 把响应吐回连接（专为代理回源而设）；
	// 它**不负责**关闭 Body，关闭是调用方的责任。
	_ = resp.Write(conn)
	_ = resp.Body.Close()
}

// TestPickEgress 锁"取众数而非取第一个"。起因是真机实测：同一台 SOCKS5 下
// ifconfig.me 报 117.157.x，而 icanhazip/3322/oray 三源一致报 36.142.x——按第一个
// 成功的源取值会把少数派当结论报出去，用户只能怀疑工具测错了（机主就撞上过）。
func TestPickEgress(t *testing.T) {
	p := func(source, ip string) EgressProbe { return EgressProbe{Source: source, IP: ip} }

	cases := []struct {
		name      string
		probes    []EgressProbe
		wantIP    string
		wantSrc   string
		wantAgree int
		wantTotal int
	}{
		{
			name:      "少数派排前面也必须取众数",
			probes:    []EgressProbe{p("ifconfig.me", "117.157.14.182"), p("3322", "36.142.0.1"), p("oray", "36.142.0.1")},
			wantIP:    "36.142.0.1",
			wantSrc:   "3322",
			wantAgree: 2, wantTotal: 3,
		},
		{
			name:      "失败源不计入总票",
			probes:    []EgressProbe{{Source: "ipify", Error: "host unreachable"}, p("3322", "36.142.0.1")},
			wantIP:    "36.142.0.1",
			wantSrc:   "3322",
			wantAgree: 1, wantTotal: 1,
		},
		{
			name:      "全不一致时取先出现的那个，票数为 1",
			probes:    []EgressProbe{p("a", "1.1.1.1"), p("b", "2.2.2.2")},
			wantIP:    "1.1.1.1",
			wantSrc:   "a",
			wantAgree: 1, wantTotal: 2,
		},
		{
			name:      "全失败则无结论",
			probes:    []EgressProbe{{Source: "a", Error: "refused"}, {Source: "b", Error: "refused"}},
			wantIP:    "",
			wantAgree: 0, wantTotal: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip, src, agree, total := pickEgress(c.probes)
			if ip != c.wantIP || src != c.wantSrc || agree != c.wantAgree || total != c.wantTotal {
				t.Fatalf("pickEgress() = (%q,%q,%d,%d)，want (%q,%q,%d,%d)",
					ip, src, agree, total, c.wantIP, c.wantSrc, c.wantAgree, c.wantTotal)
			}
		})
	}
}

// TestCanonicalAddrHostForms 锁主机栏的 IPv4 / IPv6 / 域名三种形态。
// 两个方向都会咬人：曾用"含冒号即拒"当"塞了端口"的判据，把 ::1 一并拒掉；
// 反过来，URL 里的 IPv6 不补方括号，url.Parse 会把最后一段冒号内容当端口，
// 地址直接错位。所以既验拼出的字符串，也把它喂回 netx.ParseProxy 验能被解对。
func TestCanonicalAddrHostForms(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"IPv4", "1.1.1.1", "socks5://1.1.1.1:1080"},
		{"域名", "proxy.example.com", "socks5://proxy.example.com:1080"},
		{"IPv6 裸写", "::1", "socks5://[::1]:1080"},
		{"IPv6 从命令行抄来的带括号形态", "[2408:8274::1]", "socks5://[2408:8274::1]:1080"},
		{"IPv6 链路本地", "fe80::1", "socks5://[fe80::1]:1080"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ProxyCheckRequest{Kind: "socks5", Host: c.host, Port: 1080}.canonicalAddr()
			if err != nil {
				t.Fatalf("%q 应通过，实得 err=%v", c.host, err)
			}
			if got != c.want {
				t.Fatalf("canonicalAddr() = %q, want %q", got, c.want)
			}
			p, pErr := netx.ParseProxy(got)
			if pErr != nil {
				t.Fatalf("%q 拼出的地址 netx 解不动: %v", c.host, pErr)
			}
			if p.Host != strings.TrimPrefix(c.want, "socks5://") {
				t.Errorf("解析回的 Host = %q, want %q", p.Host, strings.TrimPrefix(c.want, "socks5://"))
			}
		})
	}

	// 真塞了端口的形态必须拦住，否则右侧端口栏形同虚设
	for _, h := range []string{"1.1.1.1:22", "[::1]:22", "socks5://1.1.1.1", "u@1.1.1.1"} {
		if got, err := (ProxyCheckRequest{Kind: "socks5", Host: h, Port: 1080}).canonicalAddr(); err == nil {
			t.Errorf("%q 不该通过主机栏校验，实得 %q", h, got)
		}
	}
}
