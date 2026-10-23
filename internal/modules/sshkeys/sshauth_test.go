package sshkeys

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSH 凭据测试的用例面。这里用 x/crypto 的 **server 侧**起一个真 SSH 服务端
// （临时 ed25519 host key），所以走的是完整协议握手与认证，不是桩出来的假象。
// 整套仍落在本机回环上，不碰外网。

const (
	testSSHUser  = "zifeiyu"
	testSSHPass  = "right-p@ss:word" // 故意带 @ 与 : ，顺带验证凭据打码不漏
	testSSHWrong = "wrong-pass"
)

// sshServerFixture 起一个只做认证、不开 channel 的 SSH 服务端（默认回环 IPv4）。
// 返回监听地址与它的 host key 指纹——指纹要能对上，才说明回调真被调过。
func sshServerFixture(t *testing.T, accept func(user, pass string) bool, allowPassword bool) (addr, fingerprint string) {
	t.Helper()
	return sshServerFixtureOn(t, "tcp", "127.0.0.1:0", accept, allowPassword)
}

// sshServerFixtureOn 同上，可指定监听网络（IPv6 用例要 tcp6）。
func sshServerFixtureOn(t *testing.T, network, listenAddr string, accept func(user, pass string) bool, allowPassword bool) (addr, fingerprint string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{}
	if allowPassword {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if accept != nil && accept(c.User(), string(pass)) {
				return &ssh.Permissions{}, nil
			}
			// 这里刻意不带密码：服务端返回值会被客户端侧的错误文本引用，
			// 把凭据写进去等于自己造一条泄露路径
			return nil, fmt.Errorf("password rejected for %q", c.User())
		}
	} else {
		// allowPassword=false 要的是"只接受 publickey"的真实形态。**必须挂上
		// PublicKeyCallback**：什么都不设的假服务端会直接掐断连接（客户端读到
		// connection reset），而真实 OpenSSH 在这种配置下回的是 USERAUTH_FAILURE +
		// 剩余方法列表 [publickey]，客户端拿到的是"认证被拒"。少了这一支，用例测的
		// 就不是它声称的那个场景。
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("未登记的公钥")
		}
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen(network, listenAddr)
	if err != nil {
		t.Fatalf("起假 SSH 服务端失败（%s %s）: %v", network, listenAddr, err)
	}
	go func() {
		for {
			c, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go serveSSHHandshake(c, cfg)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

// serveSSHHandshake 完成 KEX + 认证后拒绝一切 channel：本能力只测认证，不提供服务。
func serveSSHHandshake(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		_ = nc.Reject(ssh.Prohibited, "此假服务端只做认证，不提供 channel")
	}
}

func sshTarget(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, pErr := atoiPort(portStr)
	if pErr != nil {
		t.Fatal(pErr)
	}
	return host, port
}

func TestCheckSSHAuthLoginOK(t *testing.T) {
	addr, fp := sshServerFixture(t, func(u, p string) bool {
		return u == testSSHUser && p == testSSHPass
	}, true)
	host, port := sshTarget(t, addr)

	res, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: host, Port: port, User: testSSHUser, Pass: testSSHPass, TimeoutMs: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != SSHAuthLoginOK || !res.CanLogin {
		t.Fatalf("正确凭据应判可登录，实得 %+v", res)
	}
	if res.HostKeyFingerprint != fp {
		t.Errorf("主机密钥指纹应与服务端一致: got %q want %q", res.HostKeyFingerprint, fp)
	}
	if !strings.HasPrefix(res.HostKeyType, "ssh-ed25519") {
		t.Errorf("密钥类型 = %q", res.HostKeyType)
	}
}

// TestCheckSSHAuthWrongPasswordKeepsFingerprint 锁两件容易做错的事：
// ① 密码错要判 wrong-credentials，而不是 unreachable（它确实连上了）；
// ② 认证失败时主机密钥指纹必须还在——"这台机器是不是它"与"密码对不对"是两个问题，
//
//	后者失败不该把前者的证据一起丢掉。
func TestCheckSSHAuthWrongPasswordKeepsFingerprint(t *testing.T) {
	addr, fp := sshServerFixture(t, func(u, p string) bool {
		return u == testSSHUser && p == testSSHPass
	}, true)
	host, port := sshTarget(t, addr)

	res, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: host, Port: port, User: testSSHUser, Pass: testSSHWrong, TimeoutMs: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != SSHAuthWrongCreds || res.CanLogin {
		t.Fatalf("错密码应判凭据被拒，实得 %+v", res)
	}
	if res.HostKeyFingerprint != fp {
		t.Errorf("认证失败也该保留指纹，实得 %q want %q", res.HostKeyFingerprint, fp)
	}
	// 服务端可能压根不收密码方式，客户端侧分不出来，Reason 必须把两种可能都说到
	if !strings.Contains(res.Reason, "不接受密码登录") {
		t.Errorf("Reason 应交代另一种可能，实得 %q", res.Reason)
	}
}

// TestCheckSSHAuthPasswordNotAllowed 只接受 publickey 的服务器：结果与"密码错"同为被拒。
// 这条锁的是**实现没有假装能区分它们**——那信息 x/crypto/ssh 压根没导出。
func TestCheckSSHAuthPasswordNotAllowed(t *testing.T) {
	addr, _ := sshServerFixture(t, nil, false)
	host, port := sshTarget(t, addr)

	res, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: host, Port: port, User: testSSHUser, Pass: testSSHPass, TimeoutMs: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CanLogin {
		t.Fatalf("服务端不收密码方式却判可登录，说明认证根本没跑: %+v", res)
	}
	if res.Verdict != SSHAuthWrongCreds {
		t.Errorf("应落在 wrong-credentials，实得 %q / %q", res.Verdict, res.Reason)
	}
	if !strings.Contains(res.Reason, "不接受密码登录") {
		t.Errorf("Reason 必须把这一可能写出来，实得 %q", res.Reason)
	}
}

func TestCheckSSHAuthNotSSHPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "not ssh")
	}))
	defer srv.Close()
	host, port := sshTarget(t, strings.TrimPrefix(srv.URL, "http://"))

	res, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: host, Port: port, User: testSSHUser, Pass: testSSHPass, TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CanLogin || res.Verdict == SSHAuthLoginOK {
		t.Errorf("普通 HTTP 端口不该判可登录: %+v", res)
	}
	if res.Verdict != SSHAuthUnreachable {
		t.Errorf("应判会话没谈成，实得 %q / %q", res.Verdict, res.Reason)
	}
}

func TestCheckSSHAuthRejectsBadForm(t *testing.T) {
	cases := []struct {
		name    string
		req     SSHAuthRequest
		wantErr string
	}{
		{"没填用户名", SSHAuthRequest{Host: "127.0.0.1", Port: 22, Pass: "x"}, "请填写用户名"},
		// 空密码单独拦：否则"被拒绝"会被读成"密码不对"，而真相是人没输
		{"没填密码", SSHAuthRequest{Host: "127.0.0.1", Port: 22, User: "u"}, "请填写密码"},
		{"主机栏塞了端口", SSHAuthRequest{Host: "1.1.1.1:22", Port: 22, User: "u", Pass: "x"}, "只填 IP 或域名"},
		{"端口越界", SSHAuthRequest{Host: "1.1.1.1", Port: 70000, User: "u", Pass: "x"}, "1~65535"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := newProber().CheckSSHAuth(context.Background(), c.req)
			if err == nil {
				t.Fatalf("应拒绝，实得 %+v", res)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("错误应含 %q，实得 %v", c.wantErr, err)
			}
		})
	}
}

// TestCheckSSHAuthNeverLeaksPassword：密码出现在结果任何字段里都算泄露。
// Reason 会进前端、会被复制，是这条链上最容易被漏掉的一站。
func TestCheckSSHAuthNeverLeaksPassword(t *testing.T) {
	addr, _ := sshServerFixture(t, func(u, p string) bool { return false }, true)
	host, port := sshTarget(t, addr)

	res, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: host, Port: port, User: testSSHUser, Pass: testSSHPass, TimeoutMs: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	blob := fmt.Sprintf("%+v", res)
	if strings.Contains(blob, testSSHPass) {
		t.Errorf("结果泄露了密码原文: %s", blob)
	}
	if strings.Contains(res.Reason, "right-p@ss") {
		t.Errorf("Reason 泄露密码: %q", res.Reason)
	}
}

// TestCheckSSHAuthSupportsIPv6 确认凭据测试与版本测试共用同一套主机栏判据
// （sshTargetAddr）：v6 字面量在两条路上都放行，两种写法归一到同一形态。
func TestCheckSSHAuthSupportsIPv6(t *testing.T) {
	addr, fp := sshServerFixtureOn(t, "tcp6", "[::1]:0", func(u, p string) bool {
		return u == testSSHUser && p == testSSHPass
	}, true)
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := atoiPort(portStr)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{"::1", "[::1]"} {
		res, aErr := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
			Host: h, Port: port, User: testSSHUser, Pass: testSSHPass, TimeoutMs: 4000,
		})
		if aErr != nil {
			t.Fatalf("%q 应过主机栏校验: %v", h, aErr)
		}
		if res.Verdict != SSHAuthLoginOK {
			t.Errorf("%q 应判可登录，实得 %+v", h, res)
		}
		if res.HostKeyFingerprint != fp {
			t.Errorf("%q 指纹 = %q want %q", h, res.HostKeyFingerprint, fp)
		}
	}

	// 真带端口的形态仍然拦住（两种测试共用同一条判据，不许一处松一处紧）
	if _, err := newProber().CheckSSHAuth(context.Background(), SSHAuthRequest{
		Host: "[::1]:22", Port: 22, User: testSSHUser, Pass: testSSHPass,
	}); err == nil {
		t.Error("[::1]:22 不该通过主机栏校验")
	}
}

func atoiPort(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
