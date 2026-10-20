package portscan

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// SSH 连接测试的用例面。假服务端只做协议第一回合（版本横幅），与实现边界一致。

// sshFixture 一个会吐指定横幅文本的假 SSH 端点，并记录它收到的字节（用来验
// "我方身份行确实发出去了"，而不是只测了读方向）。
type sshFixture struct {
	addr     string
	received chan string
}

func startSSHFixture(t *testing.T, lines ...string) *sshFixture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	go func() {
		for {
			conn, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				for _, l := range lines {
					if _, wErr := io.WriteString(c, l); wErr != nil {
						return
					}
				}
				buf := make([]byte, 256)
				n, _ := c.Read(buf) // 对端的身份行
				if n > 0 {
					got <- string(buf[:n])
				}
			}(conn)
		}
	}()
	// 只关 listener，**不关 got 通道**：Accept 循环里可能还有在飞的连接协程要继续发，
	// 关通道会让它 panic: send on closed channel。无人接收的缓冲通道由 GC 收走即可。
	t.Cleanup(func() { _ = ln.Close() })
	return &sshFixture{addr: ln.Addr().String(), received: got}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var pi int
	if _, err := fmt.Sscanf(p, "%d", &pi); err != nil {
		t.Fatal(err)
	}
	return h, pi
}

func TestCheckSSHUsable(t *testing.T) {
	fx := startSSHFixture(t, "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu1\r\n")
	host, port := splitHostPort(t, fx.addr)

	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("真 SSH 横幅应判可达，实得 %+v", res)
	}
	if res.Protocol != "2.0" || !strings.Contains(res.Software, "OpenSSH_9.6") {
		t.Errorf("版本解析不对: proto=%q software=%q", res.Protocol, res.Software)
	}
	if !strings.HasPrefix(res.ServerVersion, "SSH-2.0-") {
		t.Errorf("原始行应留存，实得 %q", res.ServerVersion)
	}

	// 双向交换：我方身份行必须真的发出去了（只测读方向会漏掉"连上就哑"的实现）
	select {
	case got := <-fx.received:
		if !strings.Contains(got, "SSH-2.0-hanxi_1.0") {
			t.Errorf("对端应收到我方版本行，实收 %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("我方身份行没发出去，版本交换只完成了一半")
	}
}

// TestCheckSSHIgnoresDashPrefix RFC 4253 允许识别行前有以 '-' 开头的前言行，
// 必须跳过而不是当非法应答——实现里少这一支会把好端端的服务器判成"不是 SSH"。
func TestCheckSSHIgnoresDashPrefix(t *testing.T) {
	fx := startSSHFixture(t,
		"- welcome to the private node -\r\n",
		"SSH-1.99-SSH-1.5 compatible server\r\n",
	)
	host, port := splitHostPort(t, fx.addr)

	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("以 - 开头的前言行应被忽略，实得 %+v", res)
	}
	if res.Protocol != "1.99" {
		t.Errorf("旧兼容实现的协议号应如实报出，实得 %q", res.Protocol)
	}
}

// TestCheckSSHPortOpenButNotSSH 端口通、应答不像 SSH（最常见：HTTP 服务或别的东西
// 占了 22）。必须判不通并留下首行证据，否则用户只看到"连上了但不行"。
func TestCheckSSHPortOpenButNotSSH(t *testing.T) {
	fx := startSSHFixture(t, "HTTP/1.1 400 Bad Request\r\n\r\n")
	host, port := splitHostPort(t, fx.addr)

	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatalf("应答不是 SSH 不该判可达: %+v", res)
	}
	if !strings.Contains(res.Error, "不像 SSH") || !strings.Contains(res.Error, "HTTP/1.1") {
		t.Errorf("原因要带上首行证据，实得 %q", res.Error)
	}
}

// TestCheckSSHSSilentPort 接受连接但不回话（透明转发盒、被防火墙半开的端口）。
// 必须有读超时兜住——否则一次测试永久挂住，这是本类探测最常见的悬挂形态。
func TestCheckSSHSSilentPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go func(c net.Conn) {
				time.Sleep(10 * time.Second)
				_ = c.Close()
			}(c)
		}
	}()
	defer ln.Close()

	host, port := splitHostPort(t, ln.Addr().String())
	start := time.Now()
	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 700})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if res.OK {
		t.Errorf("沉默端口不该判可达: %+v", res)
	}
	if !strings.Contains(res.Error, "超时") {
		t.Errorf("应如实归因到读超时，实得 %q", res.Error)
	}
	if elapsed > 3*time.Second {
		t.Errorf("未在预算内收口，耗时 %v", elapsed)
	}
}

// TestCheckSSHAcceptedThenClosed 连上就被关（端口在听但服务已死/重启中）。
func TestCheckSSHEarlyClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			_ = c.Close()
		}
	}()
	defer ln.Close()

	host, port := splitHostPort(t, ln.Addr().String())
	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Errorf("连上即断不该判可达: %+v", res)
	}
	if !strings.Contains(res.Error, "断开") {
		t.Errorf("原因应指向对端断开，实得 %q", res.Error)
	}
}

func TestCheckSSHUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port := splitHostPort(t, ln.Addr().String())
	_ = ln.Close() // 占位即释放，确保 refused

	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Error == "" {
		t.Errorf("无人在听应判不通并给原因: %+v", res)
	}
}

func TestCheckSSHRejectsBadForm(t *testing.T) {
	cases := []struct {
		name    string
		req     SSHCheckRequest
		wantErr string
	}{
		{"主机为空", SSHCheckRequest{Port: 22}, "请填写主机地址"},
		{"主机栏塞了整条地址", SSHCheckRequest{Host: "ssh://1.1.1.1:22", Port: 22}, "只填 IP 或域名"},
		{"主机栏塞了端口", SSHCheckRequest{Host: "1.1.1.1:22", Port: 22}, "只填 IP 或域名"},
		{"主机栏塞了账号", SSHCheckRequest{Host: "root@1.1.1.1", Port: 22}, "只填 IP 或域名"},
		{"端口为 0", SSHCheckRequest{Host: "1.1.1.1"}, "1~65535"},
		{"端口越界", SSHCheckRequest{Host: "1.1.1.1", Port: 70000}, "1~65535"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := NewScanner().CheckSSH(context.Background(), c.req)
			if err == nil {
				t.Fatalf("应拒绝，实得 %+v", res)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("错误应含 %q，实得 %v", c.wantErr, err)
			}
		})
	}
}

func TestParseSSHBanner(t *testing.T) {
	cases := []struct {
		line, proto, software string
		wantErr               bool
	}{
		{"SSH-2.0-OpenSSH_9.6", "2.0", "OpenSSH_9.6", false},
		{"SSH-1.99-Cisco-1.25", "1.99", "Cisco-1.25", false},
		{"SSH-2.0-", "2.0", "", false},
		{"SSH-2.0", "", "", true},      // 缺 comment 分隔符
		{"SSH--OpenSSH", "", "", true}, // 缺协议号
	}
	for _, c := range cases {
		proto, sw, err := parseSSHBanner(c.line)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q 应判不合规范", c.line)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q 解析失败: %v", c.line, err)
			continue
		}
		if proto != c.proto || sw != c.software {
			t.Errorf("%q → (%q,%q)，want (%q,%q)", c.line, proto, sw, c.proto, c.software)
		}
	}
}

// TestReadSSHBannerCapped 对端在识别行前猛灌前言：必须在上限处收住并判非 SSH，
// 而不是把它的流体读进内存（这条护栏与代理出口的 io.LimitReader 同一个理由）。
func TestReadSSHBannerCapped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetWriteDeadline(time.Now().Add(3 * time.Second))
				for i := 0; i < 5000; i++ { // 远超 4KiB 与 64 行两道闸
					if _, wErr := fmt.Fprintf(c, "- %s\r\n", strings.Repeat("x", 100)); wErr != nil {
						return
					}
				}
			}(c)
		}
	}()
	defer ln.Close()

	host, port := splitHostPort(t, ln.Addr().String())
	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Errorf("前言灌满也不该判可达: %+v", res)
	}
	// 读满上限即收口（4KiB 封顶），而不是等到 3s 预算耗尽
	if res.LatencyMs > 2900 {
		t.Errorf("疑似未封顶读取，耗时 %dms", res.LatencyMs)
	}
}

// TestSSHResultHasNoCredentialPath 结构性确认：这个 RPC 的入参与结果里
// 没有任何可以承载口令的字段——边界写清楚，免得后来者"顺手加个密码框"就把它
// 变成又一个凭据落点。
func TestSSHResultHasNoCredentialPath(t *testing.T) {
	fx := startSSHFixture(t, "SSH-2.0-OpenSSH_9.6\r\n")
	host, port := splitHostPort(t, fx.addr)
	res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 2000})
	if err != nil {
		t.Fatal(err)
	}
	blob := fmt.Sprintf("%+v", res)
	for _, leak := range []string{"pass", "Pass", "password", "user", "User", "secret"} {
		if strings.Contains(blob, leak) {
			t.Errorf("结果结构里出现了凭据形状的字段 %q: %s", leak, blob)
		}
	}
	reqBlob := fmt.Sprintf("%+v", SSHCheckRequest{Host: host, Port: port})
	for _, leak := range []string{"pass", "Pass", "user", "User"} {
		if strings.Contains(reqBlob, leak) {
			t.Errorf("入参不该有凭据字段，发现 %q: %s", leak, reqBlob)
		}
	}
}

// 我方身份行必须按 RFC 4253 以 CRLF 结束，否则对端 ssd 会一直等着行尾。
func TestSSHClientIdentTerminated(t *testing.T) {
	fx := startSSHFixture(t, "SSH-2.0-OpenSSH_9.6\r\n")
	host, port := splitHostPort(t, fx.addr)
	if _, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: host, Port: port, TimeoutMs: 2000}); err != nil {
		t.Fatal(err)
	}
	got := <-fx.received
	if !strings.HasSuffix(got, "\r\n") {
		t.Errorf("版本行必须 CRLF 结尾（RFC 4253），实收 %q", got)
	}
}

// TestCheckSSHSupportsIPv6 锁 IPv6 字面量可用。曾经的写法是"含冒号就判塞了端口"，
// 那会把 ::1 这类地址全部拒掉——而 v6 恰是这台机器直连出口的形态（机主实况）。
// 带方括号的 `[::1]` 也要收：那是从 `ssh user@[::1]` 抄过来的常见形态。
func TestCheckSSHSupportsIPv6(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("本机无 IPv6 栈，跳过：%v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				_, _ = io.WriteString(c, "SSH-2.0-OpenSSH_9.6\r\n")
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	for _, h := range []string{"::1", "[::1]"} {
		res, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: h, Port: port, TimeoutMs: 3000})
		if err != nil {
			t.Fatalf("%q 应通过主机栏校验，实得 err=%v", h, err)
		}
		if !res.OK {
			t.Errorf("%q 应判可达，实得 %+v", h, res)
		}
		// 括号由 JoinHostPort 统一补，两种写法都归一到同一形态
		if want := net.JoinHostPort("::1", portStr); res.Addr != want {
			t.Errorf("%q 的 Addr = %q，want %q", h, res.Addr, want)
		}
	}

	// 反面：真塞了端口的两种形态仍必须拦住
	for _, h := range []string{"[::1]:22", "1.1.1.1:22"} {
		if _, err := NewScanner().CheckSSH(context.Background(), SSHCheckRequest{Host: h, Port: 22}); err == nil {
			t.Errorf("%q 带端口却过了校验，右侧端口栏就白设了", h)
		}
	}
}
