package deploy

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 用例面：全部走 x/crypto 的 **server 侧**真握手（临时 ed25519 host key、真 KEX、
// 真认证、真 channel），只有"远端 shell"是模拟的——模拟保真度由两条测试共同钉住：
// ① 收到的命令串必须逐字节等于 remoteScript；② remoteScript 本身在真 bash 下跑
// 出同一套语义（TestRemoteScriptSemanticsRealBash，没有 bash 就如实跳过）。
// 整套只落 127.0.0.1 随机端口，不碰外网。

const (
	testUser  = "zifeiyu"
	testPass  = "sup3r-s3cret-p@ss:!" // 带 @ : ! 的怪密码，顺带验证打码不漏
	testWrong = "wrong-p@ss"
	testHome  = "/home/" + testUser
)

// ---------------------------
// 内存钉库（PinStore 的测试实现，按约定放测试文件里）
// ---------------------------

type memPins struct {
	mu       sync.Mutex
	m        map[string]string
	lookups  int
	records  int
	failLook error
	failRec  error
}

func newMemPins() *memPins { return &memPins{m: map[string]string{}} }

func (p *memPins) Lookup(addr string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lookups++
	if p.failLook != nil {
		return "", p.failLook
	}
	return p.m[addr], nil
}

func (p *memPins) Record(addr, fp string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records++
	if p.failRec != nil {
		return p.failRec
	}
	p.m[addr] = fp
	return nil
}

func (p *memPins) get(addr string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[addr]
}

func (p *memPins) counts() (lookups, records int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lookups, p.records
}

// ---------------------------
// 真握手假 SSH 服务端
// ---------------------------

type sessionBehavior int

const (
	sbRun           sessionBehavior = iota // 按 remoteScript 的语义模拟执行
	sbRejectSession                        // 认证后拒绝开 session（坏形态之一）
	sbExecHang                             // 接受 exec 后永远不回（超时可达性验证）
	sbExecFail                             // 退出码 1 + stderr（模拟 set -e 触雷）
	sbExecNoMarker                         // 退出码 0 但什么都不回（远端不配合）
)

type fakeServer struct {
	addr string
	fp   string // host key 指纹

	passwordOK func(user, pass string) bool
	pubkeyOnly bool
	behavior   sessionBehavior

	connAccepted atomic.Int32
	authAttempts atomic.Int32
	execSeen     atomic.Int32 // 被真正处理的 exec 次数

	mu            sync.Mutex
	authorized    string // 模拟 authorized_keys 全文
	lastCmd       string // 收到的 exec 命令
	stdinSeen     string // 收到的 stdin 全文
	pendingMarker string // emulateScript 攒下的待回显标记
}

func startFakeServer(t *testing.T, mutate func(*fakeServer)) *fakeServer {
	t.Helper()
	s := &fakeServer{passwordOK: func(_, pass string) bool { return pass == testPass }}
	if mutate != nil {
		mutate(s)
	}

	// 注意 ed25519.GenerateKey 返回顺序是 (pub, priv, err)，先公后私。
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	s.fp = ssh.FingerprintSHA256(signer.PublicKey())

	cfg := &ssh.ServerConfig{}
	if s.pubkeyOnly {
		// 真实形态：只接受 publickey 时 OpenSSH 回 USERAUTH_FAILURE + 剩余方法列表，
		// 客户端拿到的是"认证被拒"而不是连接异常（口径同父包凭据测试的注释）。
		cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("未登记的公钥")
		}
	} else {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			s.authAttempts.Add(1)
			if s.passwordOK != nil && s.passwordOK(c.User(), string(pass)) {
				return &ssh.Permissions{}, nil
			}
			// 刻意不把口令写进返回值：服务端错误文本可能被客户端侧引用，
			// 写进去等于自己造一条泄露路径。
			return nil, fmt.Errorf("password rejected for %q", c.User())
		}
	}
	cfg.AddHostKey(signer)

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
			s.connAccepted.Add(1)
			go s.serveConn(c, cfg)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	s.addr = ln.Addr().String()
	return s
}

func (s *fakeServer) serveConn(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	home := "/home/" + sc.User()
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "只支持 session")
			continue
		}
		if s.behavior == sbRejectSession {
			_ = nc.Reject(ssh.Prohibited, "此服务端不提供 session")
			continue
		}
		ch, requests, aErr := nc.Accept()
		if aErr != nil {
			continue
		}
		go s.handleSession(ch, requests, home)
	}
}

func (s *fakeServer) handleSession(ch ssh.Channel, requests <-chan *ssh.Request, home string) {
	for req := range requests {
		switch req.Type {
		case "exec":
			var p struct{ Cmd string }
			_ = ssh.Unmarshal(req.Payload, &p)
			s.execSeen.Add(1)
			s.mu.Lock()
			s.lastCmd = p.Cmd
			s.mu.Unlock()
			_ = req.Reply(true, nil)
			if s.behavior == sbExecHang {
				return // 永不回退出状态、不关通道：执行段超时的活体标本
			}
			stdin, _ := io.ReadAll(ch) // 对应远端 k=$(cat)：一直读到半关闭
			s.mu.Lock()
			s.stdinSeen = string(stdin)
			s.mu.Unlock()
			code := s.emulateScript(string(stdin), home)
			if s.behavior == sbExecFail {
				_, _ = ch.Stderr().Write([]byte("touch: cannot touch '/.ssh': Permission denied\n"))
			}
			_, _ = ch.Write([]byte(s.markerOut()))
			_, _ = ch.SendRequest("exit-status", false,
				ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
			_ = ch.Close()
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// markerOut 取走 emulateScript 攒下的待回显标记（拆两步是为让锁的持有范围小一点）。
func (s *fakeServer) markerOut() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.pendingMarker
	s.pendingMarker = ""
	return m
}

// emulateScript 严格照 remoteScript 的 POSIX 语义模拟：$(cat) 剥行尾换行、
// grep -qxF 整行字面比较、printf '%s\n' 落一个干净换行、回显标记 + 解析后的 $t。
func (s *fakeServer) emulateScript(stdin, home string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.behavior {
	case sbExecFail:
		return 1
	case sbExecNoMarker:
		return 0
	}
	k := strings.TrimRight(stdin, "\r\n")
	t := home + "/.ssh/authorized_keys"
	if s.behavior == sbRun {
		for _, l := range strings.Split(s.authorized, "\n") {
			if l == k {
				s.pendingMarker = fmt.Sprintf("%s %s\n", markerPresent, t)
				return 0
			}
		}
		s.authorized += k + "\n"
		s.pendingMarker = fmt.Sprintf("%s %s\n", markerAdded, t)
	}
	return 0
}

func (s *fakeServer) snapshot() (authorized, lastCmd, stdinSeen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorized, s.lastCmd, s.stdinSeen
}

// ---------------------------
// 原始坏形态假端点（静默 / 连上就断 / 别的协议应答）
// ---------------------------

func startRawListener(t *testing.T, behavior func(net.Conn)) string {
	t.Helper()
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
			go behavior(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func behavSilent(c net.Conn) { // 接受连接但不回话
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	_, _ = io.Copy(io.Discard, c)
}

func behavEarlyClose(c net.Conn) { _ = c.Close() } // 连上就断

func behavHTTP(thenClose bool) func(net.Conn) { // 端口上是别的协议应答
	return func(conn net.Conn) {
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
		if thenClose {
			// 优雅收场：半关闭让垃圾字节先于 FIN 到达，再如实 EOF（模拟"应答完就关"的
			// 真实服务；直接 Close 会被 Windows 以 RST 掐掉在途字节，那是本机的粗暴，
			// 不是被测代码的问题）。
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
			time.Sleep(200 * time.Millisecond)
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}
}

// ---------------------------
// 夹具与辅助
// ---------------------------

// testKey 生成合法 ed25519 公钥的 (单行 authorized_keys 文本, SHA256 指纹)。
// MarshalAuthorizedKey 对无注释的裸钥匙会留一个悬空空格，这里先剥再补规范注释。
func testKey(t *testing.T) (line, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil) // (pub, priv, err) 先公后私
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	line = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " hanxi-deploy-test"
	return line, ssh.FingerprintSHA256(pub)
}

func writePubFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func deployTo(t *testing.T, ctx context.Context, addr, pubPath, pass string) (Result, error) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return Result{}, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Result{}, err
	}
	return Deploy(ctx, Params{
		Host: host, User: testUser, Port: port,
		Password: pass, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	}, newMemPins())
}

// pins 参数版本（钉库要单独构造/预置的用例用这个）。
func deployToPins(t *testing.T, ctx context.Context, addr string, pins PinStore, p Params) (Result, error) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return Result{}, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Result{}, err
	}
	p.Host, p.Port = host, port
	return Deploy(ctx, p, pins)
}

func mustOK(t *testing.T, res Result, err error) Result {
	t.Helper()
	if err != nil {
		t.Fatalf("Deploy 返回 error（应只在参数非法时出现）: %v", err)
	}
	return res
}

// ---------------------------
// 六条主干
// ---------------------------

// 第 1 条 + 正常路径：appended——公钥走 stdin、远端逐字节收到、authorized_keys
// 内容正确、路径如实回显、命令串里没有任何公钥字节。
func TestDeployAppendedEndToEnd(t *testing.T) {
	srv := startFakeServer(t, nil)
	pins := newMemPins()
	line, fp := testKey(t)
	pubPath := writePubFile(t, line+"\n")

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictAppended || !res.OK {
		t.Fatalf("应判 appended，实得 %+v", res)
	}
	auth, cmd, stdin := srv.snapshot()
	if auth != line+"\n" {
		t.Errorf("远端 authorized_keys 应恰好一行公钥，实得 %q", auth)
	}
	if stdin != line+"\n" {
		t.Errorf("stdin 应逐字节收到公钥行+换行，实得 %q", stdin)
	}
	if cmd != remoteScript {
		t.Errorf("远端命令必须逐字节等于 remoteScript（模拟保真度的锚点）:\n got %q\nwant %q", cmd, remoteScript)
	}
	if strings.Contains(cmd, line) {
		t.Error("命令串里出现了公钥字节——插值防线破了")
	}
	if res.AuthorizedKeysPath != testHome+"/.ssh/authorized_keys" {
		t.Errorf("写入路径应如实回显，实得 %q", res.AuthorizedKeysPath)
	}
	if res.PubKeyFingerprint != fp {
		t.Errorf("公钥指纹本地算，want %q got %q", fp, res.PubKeyFingerprint)
	}
	if res.HostKeyFingerprint != srv.fp {
		t.Errorf("host key 指纹 want %q got %q", srv.fp, res.HostKeyFingerprint)
	}
	if pins.get(srv.addr) != srv.fp {
		t.Errorf("TOFU 首见应落钉，钉库实得 %q", pins.get(srv.addr))
	}
}

// 第 4 条：幂等靠远端回显，不靠本地猜；重跑不得产生重复行。
func TestDeployIdempotentAlreadyPresent(t *testing.T) {
	srv := startFakeServer(t, nil)
	pins := newMemPins()
	line, _ := testKey(t)
	pubPath := writePubFile(t, line+"\n")
	p := Params{User: testUser, Password: testPass, PubKeyPath: pubPath, Timeout: 3 * time.Second}

	first, err := deployToPins(t, context.Background(), srv.addr, pins, p)
	mustOK(t, first, err)
	if first.Verdict != VerdictAppended {
		t.Fatalf("第一次应 appended，实得 %+v", first)
	}
	before, _, _ := srv.snapshot()

	second, err := deployToPins(t, context.Background(), srv.addr, pins, p)
	mustOK(t, second, err)
	if second.Verdict != VerdictAlreadyPresent || !second.OK {
		t.Fatalf("第二次应 already-present，实得 %+v", second)
	}
	if !strings.Contains(second.Reason, "幂等") {
		t.Errorf("Reason 应说清没动过远端，实得 %q", second.Reason)
	}
	after, _, _ := srv.snapshot()
	if after != before {
		t.Errorf("重跑后 authorized_keys 字节变了:\n before %q\n after  %q", before, after)
	}
	if strings.Count(after, line) != 1 {
		t.Errorf("不得出现重复行，实得 %q", after)
	}
}

// 第 3 条：TOFU 换钉防线——指纹与钉不符必须在**认证之前**掐断。
// 断言"服务端一次认证尝试都没收到"是这条的命门。
func TestDeployHostKeyChangedRefusesBeforeAuth(t *testing.T) {
	srv := startFakeServer(t, nil)
	pins := newMemPins()
	line, _ := testKey(t)
	pubPath := writePubFile(t, line+"\n")
	p := Params{User: testUser, Password: testPass, PubKeyPath: pubPath, Timeout: 3 * time.Second}

	// 先正常部署一次，把真钉建立起来（records==1）。
	if _, err := deployToPins(t, context.Background(), srv.addr, pins, p); err != nil {
		t.Fatal(err)
	}

	// 篡改钉库（等价于：对端换 key 了，或首见时被 MITM 过）。
	pins.mu.Lock()
	pins.m[srv.addr] = "SHA256:attacker-key"
	pins.mu.Unlock()

	res, err := deployToPins(t, context.Background(), srv.addr, pins, p)
	mustOK(t, res, err)
	if res.Verdict != VerdictHostKeyChanged || res.OK {
		t.Fatalf("应判 hostkey-changed，实得 %+v", res)
	}
	if got := srv.authAttempts.Load(); got != 1 {
		t.Errorf("换钉后不得再发起认证（首次部署应只 1 次尝试），实得 %d", got)
	}
	if got := srv.execSeen.Load(); got != 1 {
		t.Errorf("换钉后第二次会话不得开到 exec（首次部署应只 1 次执行），实得 %d", got)
	}
	if res.HostKeyFingerprint != srv.fp {
		t.Errorf("结论要保留本次实得指纹供排查，实得 %q", res.HostKeyFingerprint)
	}
	if !strings.Contains(res.Reason, "SHA256:attacker-key") || !strings.Contains(res.Reason, srv.fp) {
		t.Errorf("Reason 要同时给出钉值与实得值，实得 %q", res.Reason)
	}
	if recs := pins.recordsAfter(); recs != 1 {
		t.Errorf("不匹配时不得覆写旧钉（覆写=攻击者洗白）, records=%d", recs)
	}
}

func (p *memPins) recordsAfter() int { _, r := p.counts(); return r }

// 第 3 条（记录时点）：首见落钉刻意放在认证成功之后——口令都过不了的
// MITM 不配被钉住。
func TestDeployTOFURecordsPinOnlyAfterSuccessfulAuth(t *testing.T) {
	srv := startFakeServer(t, nil)
	line, _ := testKey(t)
	pubPath := writePubFile(t, line+"\n")

	// 首见但口令不对：不该落钉（挡一道连口令都过不了的 MITM）。
	failed, err := deployToPins(t, context.Background(), srv.addr, newMemPins(), Params{
		User: testUser, Password: testWrong, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	})
	mustOK(t, failed, err)
	if failed.Verdict != VerdictAuthRejected {
		t.Fatalf("夹具应落在 auth-rejected，实得 %+v", failed)
	}
	pins := newMemPins()
	if _, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testWrong, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if pins.get(srv.addr) != "" {
		t.Errorf("认证失败不得落钉，实得 %q", pins.get(srv.addr))
	}
	lookups, records := pins.counts()
	if lookups == 0 || records != 0 {
		t.Errorf("要查钉但不落钉: lookups=%d records=%d", lookups, records)
	}

	// 首见且认证成功：落钉。
	okPins := newMemPins()
	res, err := deployToPins(t, context.Background(), srv.addr, okPins, Params{
		User: testUser, Password: testPass, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictAppended {
		t.Fatalf("应 appended，实得 %+v", res)
	}
	if okPins.get(srv.addr) != srv.fp {
		t.Errorf("认证成功后应落真指纹钉，实得 %q", okPins.get(srv.addr))
	}
}

// 第 5 条 + 口令防线：错口令判 auth-rejected，Reason 保留两种可能，口令不落任何出口。
func TestDeployAuthRejectedWrongPassword(t *testing.T) {
	srv := startFakeServer(t, nil)
	pins := newMemPins()
	line, _ := testKey(t)
	pubPath := writePubFile(t, line+"\n")

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testWrong, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictAuthRejected || res.OK {
		t.Fatalf("应判 auth-rejected，实得 %+v", res)
	}
	if !strings.Contains(res.Reason, "不接受密码登录") {
		t.Errorf("Reason 必须把另一种可能写出来，实得 %q", res.Reason)
	}
	auth, _, _ := srv.snapshot()
	if auth != "" {
		t.Errorf("被拒的会话不得写任何东西，实得 %q", auth)
	}
	assertNoSecret(t, res)
}

// 第 5 条的姊妹形态：只允许密钥的服务器。与"密码不对"在 API 层不可区分，
// 这条锁"实现没有假装能区分它们"。
func TestDeployAuthRejectedPasswordNotAllowed(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) {
		s.pubkeyOnly = true
		s.passwordOK = nil
	})
	pins := newMemPins()
	line, _ := testKey(t)

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictAuthRejected || res.OK {
		t.Fatalf("只允许密钥应判 auth-rejected，实得 %+v", res)
	}
	if !strings.Contains(res.Reason, "不接受密码登录") {
		t.Errorf("Reason 必须把这一可能写出来，实得 %q", res.Reason)
	}
	assertNoSecret(t, res)
}

// 第 2 条 + 第 1 条的注入样本：本地拒发、零外发。
// 注意夹具是**口令正确、随时可写**的服务端——只要代码真拨了号，authorized_keys
// 就会被写脏；测完断言它没动过，等于证明"一个字节都不外发"。
func TestDeployRefusedLocalOnly(t *testing.T) {
	srv := startFakeServer(t, nil)
	line, _ := testKey(t)

	cases := []struct {
		name    string
		content string
		wantIn  string
	}{
		{"空文件", "", "为空"},
		{"只有空白", "\n  \n", "空白"},
		{"不是公钥", "hello world\n", "authorized_keys"},
		{"注入样本：换行+shell 片段", line + "\n; touch pwned\n", "换行"},
		{"注入样本：CRLF 伪单行", line + "\r\n; touch pwned\r\n", "换行"},
		{"options 后门（command=）", `restrict,command="touch pwned" ` + line, "options"},
		{"控制字符", line + "\x1b[31m", "控制字符"},
		{"私钥内容冒充 .pub", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA\n-----END OPENSSH PRIVATE KEY-----\n", "换行"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pins := newMemPins()
			res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
				User: testUser, Password: testPass,
				PubKeyPath: writePubFile(t, c.content), Timeout: 3 * time.Second,
			})
			mustOK(t, res, err)
			if res.Verdict != VerdictRefused || res.OK {
				t.Fatalf("应判 refused，实得 %+v", res)
			}
			if !strings.Contains(res.Reason, c.wantIn) {
				t.Errorf("Reason 应含 %q，实得 %q", c.wantIn, res.Reason)
			}
			if n := srv.connAccepted.Load(); n != 0 {
				t.Errorf("拒发还拨了号：服务端收到 %d 条连接", n)
			}
			if n := srv.authAttempts.Load(); n != 0 {
				t.Errorf("拒发还发起了认证：%d 次", n)
			}
			if l, _ := pins.counts(); l != 0 {
				t.Errorf("公钥没验过就查钉库，顺序不对：lookups=%d", l)
			}
			if auth, _, _ := srv.snapshot(); auth != "" {
				t.Errorf("authorized_keys 被动过: %q", auth)
			}
		})
	}

	t.Run("文件不存在", func(t *testing.T) {
		pins := newMemPins()
		res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
			User: testUser, Password: testPass,
			PubKeyPath: filepath.Join(t.TempDir(), "missing.pub"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		if res.Verdict != VerdictRefused {
			t.Fatalf("读不到的 .pub 应拒发，实得 %+v", res)
		}
	})
}

// 第 1 条（合法但面目可疑的单行）：注释里的 shell 片段原样落盘、绝不执行。
// 真 shell 下的不可执行性由 TestRemoteScriptSemanticsRealBash 钉。
func TestDeployInjectionInertSingleLine(t *testing.T) {
	srv := startFakeServer(t, nil)
	pins := newMemPins()
	line, _ := testKey(t)
	nasty := line + ` ; touch $HOME/PWNED $(id)  evil-comment`
	pubPath := writePubFile(t, nasty+"\n")

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: pubPath, Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictAppended || !res.OK {
		t.Fatalf("合法单行（注释怪）应照常部署，实得 %+v", res)
	}
	auth, cmd, stdin := srv.snapshot()
	if auth != nasty+"\n" {
		t.Errorf("公钥行应逐字节字面落盘，实得 %q", auth)
	}
	if stdin != nasty+"\n" {
		t.Errorf("怪注释必须只走 stdin，实得 %q", stdin)
	}
	if cmd != remoteScript {
		t.Error("命令串被公钥内容污染了")
	}
}

// 第 6 条：超时必须是真可达状态。三个挂死形态逐一收口。

func TestDeployTimeoutSilentPort(t *testing.T) {
	addr := startRawListener(t, behavSilent) // 接受连接但不回话
	line, _ := testKey(t)

	start := time.Now()
	res, err := deployTo(t, context.Background(), addr, writePubFile(t, line+"\n"), testPass)
	mustOK(t, res, err)
	if res.Verdict != VerdictTimeout {
		t.Errorf("静默端口应判 timeout，实得 %+v", res)
	}
	if el := time.Since(start); el > 6*time.Second {
		t.Errorf("未在预算附近收口，耗时 %v", el)
	}
}

func TestDeployEarlyClose(t *testing.T) {
	addr := startRawListener(t, behavEarlyClose) // 连上就断
	line, _ := testKey(t)

	res, err := deployTo(t, context.Background(), addr, writePubFile(t, line+"\n"), testPass)
	mustOK(t, res, err)
	if res.Verdict != VerdictUnreachable {
		t.Errorf("连上即断应判 unreachable，实得 %+v", res)
	}
}

func TestDeployNotSSH(t *testing.T) {
	line, _ := testKey(t)
	for _, closeAfter := range []bool{false, true} {
		name := "应答后保持连接"
		if closeAfter {
			name = "应答后即断"
		}
		t.Run(name, func(t *testing.T) {
			addr := startRawListener(t, behavHTTP(closeAfter))
			res, err := deployTo(t, context.Background(), addr, writePubFile(t, line+"\n"), testPass)
			mustOK(t, res, err)
			if res.Verdict != VerdictNotSSH {
				t.Errorf("HTTP 应答端口应判 not-ssh，实得 %+v", res)
			}
			if !strings.Contains(res.Reason, "HTTP/1.1") {
				t.Errorf("Reason 要留下首行证据，实得 %q", res.Reason)
			}
		})
	}
}

func TestDeployUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // 占位即释放，确保 refused

	line, _ := testKey(t)
	res, err := deployTo(t, context.Background(), addr, writePubFile(t, line+"\n"), testPass)
	mustOK(t, res, err)
	if res.Verdict != VerdictUnreachable || res.Reason == "" {
		t.Errorf("无人在听应判 unreachable 并给原因，实得 %+v", res)
	}
}

// 第 6 条（认证后段）："认证后不开 session" / "exec 后永不回话" /
// "退出码非 0" / "退出码 0 但没标记"——四种坏形态都必须有结论而不是干等。

func TestDeployNoSessionAfterAuth(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) { s.behavior = sbRejectSession })
	pins := newMemPins()
	line, _ := testKey(t)

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictUnreachable {
		t.Fatalf("认证后不开 session 应判 unreachable，实得 %+v", res)
	}
	if !strings.Contains(res.Reason, "会话") {
		t.Errorf("Reason 应说明会话没开成，实得 %q", res.Reason)
	}
	if res.HostKeyFingerprint != srv.fp {
		t.Error("连上过的证据（指纹）不得丢")
	}
}

func TestDeployExecHangTimeout(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) { s.behavior = sbExecHang })
	pins := newMemPins()
	line, _ := testKey(t)

	start := time.Now()
	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 800 * time.Millisecond,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictTimeout {
		t.Fatalf("远端不回执行结果应判 timeout，实得 %+v", res)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("执行段未收口，耗时 %v", el)
	}
}

func TestDeployContextCancel(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) { s.behavior = sbExecHang })
	pins := newMemPins()
	line, _ := testKey(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(400 * time.Millisecond); cancel() }()
	start := time.Now()
	res, err := deployToPins(t, ctx, srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 30 * time.Second,
	})
	mustOK(t, res, err)
	if time.Since(start) > 8*time.Second {
		t.Fatal("ctx 取消没把执行段叫醒，还在干等 30s 预算")
	}
	if res.OK {
		t.Errorf("取消不该带成功结论: %+v", res)
	}
	if res.Reason == "" {
		t.Error("取消也要如实留原因")
	}
}

func TestDeployExecFailureExitStatus(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) { s.behavior = sbExecFail })
	pins := newMemPins()
	line, _ := testKey(t)

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictUnreachable {
		t.Fatalf("远端 set -e 触雷应判 unreachable，实得 %+v", res)
	}
	if !strings.Contains(res.Reason, "退出码") || !strings.Contains(res.Reason, "Permission denied") {
		t.Errorf("Reason 应带退出码与 stderr 摘录，实得 %q", res.Reason)
	}
}

func TestDeployExecNoMarker(t *testing.T) {
	srv := startFakeServer(t, func(s *fakeServer) { s.behavior = sbExecNoMarker })
	pins := newMemPins()
	line, _ := testKey(t)

	res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
		User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
	})
	mustOK(t, res, err)
	if res.Verdict != VerdictUnreachable || res.OK {
		t.Fatalf("没拿到标记不得冒充成功，实得 %+v", res)
	}
	if !strings.Contains(res.Reason, "标记") {
		t.Errorf("Reason 应说明缺标记，实得 %q", res.Reason)
	}
}

// ---------------------------
// 第 5 条：口令不进返回值（JSON 化后搜）
// ---------------------------

func assertNoSecret(t *testing.T, res Result) {
	t.Helper()
	blob, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{testPass, "sup3r", "s3cret", "p@ss"} {
		if strings.Contains(string(blob), s) || strings.Contains(fmt.Sprintf("%+v", res), s) {
			t.Errorf("结果泄露口令片段 %q: %s", s, blob)
		}
	}
}

func TestDeployNeverLeaksPassword(t *testing.T) {
	line, _ := testKey(t)

	t.Run("auth-rejected", func(t *testing.T) {
		srv := startFakeServer(t, nil)
		res, err := deployToPins(t, context.Background(), srv.addr, newMemPins(), Params{
			User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		assertNoSecret(t, res)
	})
	t.Run("appended", func(t *testing.T) {
		srv := startFakeServer(t, nil)
		res, err := deployToPins(t, context.Background(), srv.addr, newMemPins(), Params{
			User: testUser, Password: testWrong, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		assertNoSecret(t, res)
	})
	t.Run("hostkey-changed", func(t *testing.T) {
		pins := newMemPins()
		srv := startFakeServer(t, nil)
		pins.mu.Lock()
		pins.m[srv.addr] = "SHA256:whatever-mismatch"
		pins.mu.Unlock()
		res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
			User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		if res.Verdict != VerdictHostKeyChanged {
			t.Fatalf("夹具应触发换钉分支，实得 %+v", res)
		}
		assertNoSecret(t, res)
	})
}

// ---------------------------
// 钉库故障：读写失败都必须在对外动手之前收口
// ---------------------------

func TestDeployPinStoreFailures(t *testing.T) {
	line, _ := testKey(t)

	t.Run("Lookup 故障拒发零外发", func(t *testing.T) {
		srv := startFakeServer(t, nil)
		pins := newMemPins()
		pins.failLook = fmt.Errorf("钉库文件坏了")
		res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
			User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		if res.Verdict != VerdictRefused {
			t.Fatalf("钉库读不动就没有 TOFU，必须拒发，实得 %+v", res)
		}
		if n := srv.connAccepted.Load(); n != 0 {
			t.Errorf("Lookup 失败还拨了号，连接数 %d", n)
		}
	})

	t.Run("Record 故障在写入前中止", func(t *testing.T) {
		srv := startFakeServer(t, nil)
		pins := newMemPins()
		pins.failRec = fmt.Errorf("数据根只读")
		res, err := deployToPins(t, context.Background(), srv.addr, pins, Params{
			User: testUser, Password: testPass, PubKeyPath: writePubFile(t, line+"\n"), Timeout: 3 * time.Second,
		})
		mustOK(t, res, err)
		if res.Verdict != VerdictRefused {
			t.Fatalf("钉不住就不写（fail-closed），实得 %+v", res)
		}
		if auth, _, _ := srv.snapshot(); auth != "" {
			t.Errorf("Record 失败后 authorized_keys 竟被写了: %q", auth)
		}
	})
}

// ---------------------------
// 参数通道（没能开测）
// ---------------------------

func TestDeployParamValidation(t *testing.T) {
	line, _ := testKey(t)
	pubPath := writePubFile(t, line+"\n")
	base := Params{Host: "127.0.0.1", User: testUser, Port: 22, Password: testPass,
		PubKeyPath: pubPath, Timeout: time.Second}

	cases := []struct {
		name   string
		mutate func(*Params)
		wantIn string
	}{
		{"主机为空", func(p *Params) { p.Host = "" }, "主机地址"},
		{"主机栏塞了端口", func(p *Params) { p.Host = "1.1.1.1:22" }, "只填 IP 或域名"},
		{"端口越界", func(p *Params) { p.Port = 70000 }, "1~65535"},
		{"用户名为空", func(p *Params) { p.User = " " }, "用户名"},
		{"密码为空", func(p *Params) { p.Password = "" }, "密码"},
		{"公钥路径为空", func(p *Params) { p.PubKeyPath = "" }, "公钥路径"},
		// 后缀硬校验：指错到私钥文件（无 .pub）在**读取之前**就被拦下
		{"路径不是 .pub", func(p *Params) { p.PubKeyPath = filepath.Join(filepath.Dir(pubPath), "id_ed25519") }, ".pub"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := base
			c.mutate(&p)
			res, err := Deploy(context.Background(), p, newMemPins())
			if err == nil {
				t.Fatalf("应走参数错误，实得 %+v", res)
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("错误应含 %q，实得 %v", c.wantIn, err)
			}
			if strings.Contains(err.Error(), testPass) {
				t.Errorf("错误文本带了口令: %v", err)
			}
		})
	}

	t.Run("PinStore 未注入", func(t *testing.T) {
		if _, err := Deploy(context.Background(), base, nil); err == nil ||
			!strings.Contains(err.Error(), "PinStore") {
			t.Errorf("没有钉库宁可不做，实得 %v", err)
		}
	})
}

// ---------------------------
// 单元层辅助断言
// ---------------------------

func TestTargetAddrForm(t *testing.T) {
	// 钉库键 = 拨号地址，形态必须稳定（IPv6 归一到带括号）。
	for _, in := range []string{"::1", "[::1]"} {
		addr, err := targetAddr(in, 22)
		if err != nil || addr != "[::1]:22" {
			t.Errorf("%q → (%q, %v)，want [::1]:22", in, addr, err)
		}
	}
	if addr, err := targetAddr("example.com", 2222); err != nil || addr != "example.com:2222" {
		t.Errorf("域名形态 → (%q, %v)", addr, err)
	}
}

func TestScanMarkers(t *testing.T) {
	v, path, ok := scanMarkers([]byte("HANXI_ADDED /home/u/.ssh/authorized_keys\n"))
	if !ok || v != VerdictAppended || path != "/home/u/.ssh/authorized_keys" {
		t.Errorf("带路径的 ADDED 没读对: %v %v", v, path)
	}
	v, path, ok = scanMarkers([]byte("banner 噪音\nHANXI_PRESENT\n"))
	if !ok || v != VerdictAlreadyPresent || path != defaultKeysPath {
		t.Errorf("缺路径时要落回约定形态: %v %v", v, path)
	}
	if _, _, ok := scanMarkers([]byte("无标记输出")); ok {
		t.Error("没有标记应判 found=false")
	}
}

// ---------------------------
// 模拟保真度的另一半：remoteScript 在真 shell 下的语义
// ---------------------------

// TestRemoteScriptSemanticsRealBash 用真 bash 直接执行 remoteScript（HOME 指向
// 临时目录），验证模拟所依赖的全部 shell 语义：$(cat) 剥换行、grep -qxF 幂等、
// printf 落单行、怪注释不可执行、chmod 不炸。没有 bash 就如实跳过（不假装验过）。
func TestRemoteScriptSemanticsRealBash(t *testing.T) {
	bash := findBash(t)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "precious"), []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	line, _ := testKey(t)
	nasty := line + ` ; touch PWNED $(touch PWNED2) evil`
	keysFile := filepath.Join(home, ".ssh", "authorized_keys")

	run := func() string {
		cmd := exec.Command(bash, "--noprofile", "--norc", "-c", remoteScript)
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(nasty + "\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("remoteScript 在真 bash 下失败: %v\n%s", err, out)
		}
		return string(out)
	}

	first := run()
	if !strings.Contains(first, markerAdded) {
		t.Fatalf("首次应回显 ADDED 标记，实得 %q", first)
	}
	got, err := os.ReadFile(keysFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != nasty+"\n" {
		t.Errorf("authorized_keys 应逐字面存那行公钥，实得 %q", got)
	}

	second := run()
	if !strings.Contains(second, markerPresent) {
		t.Fatalf("重跑应回显 PRESENT 标记，实得 %q", second)
	}
	got2, _ := os.ReadFile(keysFile)
	if string(got2) != string(got) {
		t.Errorf("重跑不得改动文件:\n before %q\n after  %q", got, got2)
	}

	// 注入不成立的硬证明：若 shell 解析过公钥内容，这两个文件会存在。
	for _, bad := range []string{"PWNED", "PWNED2"} {
		if _, err := os.Stat(filepath.Join(home, bad)); err == nil {
			t.Errorf("公钥注释里的 %q 被执行了——stdin 防线破了", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "precious")); err != nil {
		t.Errorf("目录里不该动的文件没了: %v", err)
	}
}

func findBash(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	for _, cand := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Skip("本机找不到 bash，跳过真 shell 语义验证（模拟保真度只剩 cmd==remoteScript 一条锚，如实声明）")
	return ""
}
