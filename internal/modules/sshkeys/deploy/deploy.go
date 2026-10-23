// Package deploy 把一把本地公钥部署到远端机器的 authorized_keys（以一次性口令换会话）。
//
// 与同域"探测"（SSH 连接测试 / 凭据测试）的根本不同，是本包存在的理由之一：
// 探测只是"看一眼"，不产生写影响，所以它可以刻意不校验 host key、把指纹原样交回
// 让人自己去对。部署公钥是**对远端的写操作**，执行这次写的会话还是用密码换来的——
// 若沿用"不校验"，MITM 之下我们会把公钥交到攻击者的机器上，从此他"授权"了我们
// 以为的那台目标机的后续免密登录。因此本包**必须做 TOFU 钉定**（Trust-On-First-Use）：
// 首次见到的 host:port 记录指纹并放行（与 OpenSSH 同口径：**认证成功之后**才记录，
// 挡一道连口令都过不了的 MITM）；此后指纹与钉不符则在**认证之前**就拒绝——连密码
// 都不发出去。
//
// 边界（都是刻意的，不是疏漏）：
//   - 公钥内容只走 stdin 送达远端，绝不做字符串插值拼进远端命令——pub 文件可能
//     被人动过手脚（塞换行 + "; rm -rf ~"），拼进命令串就等于我们自己在人家机器上
//     执行注入；发送前先在本地验 authorized_keys 单行形态，验不过一个字节都不外发。
//   - 绝不读私钥：路径必须 .pub 结尾，内容必须过 ssh.ParseAuthorizedKey。
//   - 口令只活在这一次调用里：不进 Result、不进 Reason/错误文本（一律打码）、不落盘。
//   - 钉定信息存哪儿是父包/数据根的事，经 PinStore 注入。本包因此只依赖标准库与
//     x/crypto/ssh，不 import 仓内任何包，可独立测试。
//
// 结论口径（Result.Verdict，互斥）：
//
//	appended          公钥已写入（本次新增）
//	already-present   公钥已在 authorized_keys 里（幂等重跑，远端没动）
//	auth-rejected     认证被拒（"密码不对"与"服务器不接受密码"在 API 层不可区分，见 Reason）
//	unreachable       连不上；或连上了但 SSH 会话/远端执行没谈成
//	timeout           预算耗尽（KEX/认证/执行任一段）
//	not-ssh           端口通，但应答不是 SSH
//	hostkey-changed   指纹与钉不符，认证之前就拒绝（什么都没发出去）
//	refused           本地拒发（公钥形态非法/空、PinStore 读写失败、参数问题）
package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Verdict 取值。与 Result 一起构成这个能力的全部对外语言。
const (
	VerdictAppended       = "appended"
	VerdictAlreadyPresent = "already-present"
	VerdictAuthRejected   = "auth-rejected"
	VerdictUnreachable    = "unreachable"
	VerdictTimeout        = "timeout"
	VerdictNotSSH         = "not-ssh"
	// VerdictHostKeyChanged 是 TOFU 的核心防线：与钉不符时在认证之前拒绝。
	VerdictHostKeyChanged = "hostkey-changed"
	// VerdictRefused 专指"本地就没让出门"——一个字节都未对外发送。
	VerdictRefused = "refused"
)

const (
	// defaultTimeout 是 Params.Timeout <= 0 时的兜底预算（单段，见 Deploy 注释）。
	defaultTimeout = 15 * time.Second

	// maxPubFileBytes 公钥文件读取上限。合法 authorized_keys 单行最大约几 KB
	// （RSA-16384 也不过 ~7KB），超限即异常形态，拒发比继续处理安全。
	maxPubFileBytes = 16 * 1024

	// recordCap 只保留对端前 4KiB 字节，用于把"端口上是别的协议"归因成 not-ssh。
	recordCap = 4096

	// markerAdded / markerPresent 是远端脚本的回显标记（后随实际写入路径）。
	// 幂等判定靠远端回显 HANXI_PRESENT，不靠本地猜。
	markerAdded   = "HANXI_ADDED"
	markerPresent = "HANXI_PRESENT"

	// defaultKeysPath 远端没回显路径时如实写出的约定形态。
	defaultKeysPath = "~/.ssh/authorized_keys"
)

// remoteScript 是发到远端执行的 POSIX sh 片段。
//
// 关键设计：公钥文本**不在这条命令里**，而是从 stdin 读（k=$(cat)）。命令串本身
// 固定不变、不含任何插值槽位——公钥文件内容再怎么恶意（分号、$()、反引号），最多
// 成为 grep -qxF 的字面匹配串和 printf 的字面参数，永远轮不到 shell 解析。
// k=$(cat) 剥掉行尾换行、printf '%s\n' 恰好补回一个，authorized_keys 里存的是
// 干净的单行公钥；grep -qxF 做整行字面比较实现幂等。
const remoteScript = `set -e; d=$HOME/.ssh; mkdir -p "$d" && chmod 700 "$d"; ` +
	`t="$d/authorized_keys"; touch "$t" && chmod 600 "$t"; k=$(cat); ` +
	`if grep -qxF "$k" "$t"; then echo HANXI_PRESENT "$t"; ` +
	`else printf '%s\n' "$k" >> "$t"; echo HANXI_ADDED "$t"; fi`

// Params 一次公钥部署的入参。
type Params struct {
	Host string
	User string
	Port int
	// Password 一次性：只为换取这一次会话，不进 Result、不进错误文本（打码后除外）、不落盘。
	Password string
	// PubKeyPath 指向 .pub 公钥文件；本包绝不读私钥（后缀硬校验 + 内容形态硬校验）。
	PubKeyPath string
	// Timeout 是**每一段**的预算（连接/KEX+认证一段、会话执行一段），<=0 用默认值。
	Timeout time.Duration
}

// Result 部署结果。没有承载口令的字段；Reason 里的远端错误文本一律先打码。
type Result struct {
	Verdict            string
	OK                 bool // 仅 appended / already-present
	Reason             string
	HostKeyFingerprint string
	// AuthorizedKeysPath 如实回显远端写入路径（远端脚本 echo 的是解析后的 $t，
	// 含 $HOME 实际值），机主需要在界面上看到"写到哪了"。
	AuthorizedKeysPath string
	PubKeyFingerprint  string
}

// PinStore 由调用方注入：本包不决定钉定信息存哪儿（那是父包/数据根的事），
// 也因此不 import 仓内任何包。
type PinStore interface {
	// Lookup 返回该 host:port 已记录的指纹；无记录返回空串。
	Lookup(addr string) (string, error)
	Record(addr, fp string) error
}

// Deploy 执行一次公钥部署。error 通道只装"没能开测"（参数非法）；
// 连不上、被拒、换钉这些全是正常结论，落在 Result.Verdict。
//
// 超时分段：拨号+KEX+认证共用一条 deadline（预算 x），认证成功后重设一条新 deadline
// 覆盖会话执行（再给 x）——x/crypto 的 KEX/认证吃 cfg.Timeout 与底层 conn deadline，
// 会话执行则只能靠后者，不重设的话"认证慢一点"就会饿死执行段。
func Deploy(ctx context.Context, p Params, pins PinStore) (Result, error) {
	addr, err := targetAddr(p.Host, p.Port)
	if err != nil {
		return Result{}, err
	}
	user := strings.TrimSpace(p.User)
	if user == "" {
		return Result{}, errors.New("请填写用户名")
	}
	if p.Password == "" {
		return Result{}, errors.New("请填写密码（本能力用一次性口令换取写会话；空密码只会得出误导性的「被拒绝」结论）")
	}
	pubPath := strings.TrimSpace(p.PubKeyPath)
	if pubPath == "" {
		return Result{}, errors.New("请填写公钥路径（.pub）")
	}
	// 后缀硬校验挡在读取之前：这不是防"手滑"，是把"绝不读私钥"写成可执行的约束。
	if !strings.HasSuffix(strings.ToLower(pubPath), ".pub") {
		return Result{}, errors.New("公钥路径必须以 .pub 结尾——本能力绝不读私钥文件")
	}
	if pins == nil {
		return Result{}, errors.New("PinStore 未注入：TOFU 钉定必须有可写的钉库，没有宁可不做")
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	res := Result{}

	// 第一步永远是本地验货：形态不过，一个字节都不外发。
	line, pubFP, refusal := loadPubKeyLine(pubPath)
	res.PubKeyFingerprint = pubFP
	if refusal != "" {
		res.Verdict, res.Reason = VerdictRefused, refusal
		return res, nil
	}

	// 查钉。查不动钉库就没法保证 TOFU，同样拒发。
	pinned, pErr := pins.Lookup(addr)
	if pErr != nil {
		res.Verdict = VerdictRefused
		res.Reason = "钉库读取失败，无法保证 TOFU，部署中止（未外发任何字节）: " + pErr.Error()
		return res, nil
	}

	dialer := &net.Dialer{Timeout: timeout}
	netConn, dErr := dialer.DialContext(ctx, "tcp", addr)
	if dErr != nil {
		res.Verdict, res.Reason = classifyDialError(dErr)
		return res, nil
	}
	defer netConn.Close()

	rec := &recordingConn{Conn: netConn}
	// 这套 API 不吃 ctx，KEX 与认证靠 conn deadline 兜住。
	_ = netConn.SetDeadline(time.Now().Add(timeout))

	var captured struct {
		fingerprint string
		mismatched  bool
	}
	cfg := &ssh.ClientConfig{
		User:    user,
		Auth:    []ssh.AuthMethod{ssh.Password(p.Password)},
		Timeout: timeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured.fingerprint = ssh.FingerprintSHA256(key)
			if pinned != "" && captured.fingerprint != pinned {
				// 不解释、不宽容：直接掐断 KEX。后面的"不发起认证"测试钉住这一条。
				captured.mismatched = true
				return errors.New("host key 与钉库不符")
			}
			// pinned == ""：TOFU 首见，放行；但**认证成功之前不落钉**（见下）。
			return nil
		},
	}

	sshConn, chans, reqs, connErr := ssh.NewClientConn(rec, addr, cfg)
	res.HostKeyFingerprint = captured.fingerprint
	if connErr != nil {
		if captured.mismatched {
			res.Verdict = VerdictHostKeyChanged
			res.OK = false
			res.Reason = fmt.Sprintf("host key 与钉库不符：钉定 %s，本次实得 %s。"+
				"已在认证之前拒绝（密码未发出）。若对端确系重装，请清除该地址的钉后重新部署。",
				pinned, captured.fingerprint)
			return res, nil
		}
		res.Verdict, res.Reason = classifyHandshake(connErr, rec.snapshot(), p.Password)
		return res, nil
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	// TOFU 记录点。刻意放在认证成功之后：拿口令都过不了的 MITM 不配被钉住；
	// 而口令过了说明对面至少在"它知道自己是谁"这条证据链上走完了 SSH 认证。
	if pinned == "" {
		if rErr := pins.Record(addr, captured.fingerprint); rErr != nil {
			// fail-closed：钉不住就不做无钉保证的写入。认证已经发生（口令已送达
			// 我们核验过的这台机器），但 authorized_keys 未动。
			res.Verdict = VerdictRefused
			res.Reason = "钉库写入失败，部署在对远端做任何写入之前中止（fail-closed，authorized_keys 未被改动）: " + rErr.Error()
			return res, nil
		}
	}

	// 执行段：重设 deadline（见 Deploy 注释），ctx 取消用"关连接"落地，
	// 因为会话层同样不吃 ctx。
	phaseDeadline := time.Now().Add(timeout)
	_ = netConn.SetDeadline(phaseDeadline)
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-watchDone:
		}
	}()

	session, sErr := client.NewSession()
	if sErr != nil {
		res.Verdict, res.Reason = classifySession(sErr, ctx, p.Password, time.Time{})
		res.Reason = "认证通过但会话没开成: " + res.Reason
		return res, nil
	}
	defer session.Close()

	in, inErr := session.StdinPipe()
	if inErr != nil {
		res.Verdict, res.Reason = classifySession(inErr, ctx, p.Password, time.Time{})
		res.Reason = "认证通过但公钥通道没建起来: " + res.Reason
		return res, nil
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	// 公钥走 stdin。这就是第 1 条的全部内容：命令串自始至终是 remoteScript，
	// 不含公钥文本的任何字节。
	go func() {
		_, _ = io.WriteString(in, line+"\n")
		_ = in.Close()
	}()

	runErr := session.Run(remoteScript)
	if runErr != nil {
		var ex *ssh.ExitError
		if errors.As(runErr, &ex) {
			// 收到明确退出码说明远端在预算内跑完了——set -e 触雷（mkdir/chmod/touch
			// 任一失败）就是这一支。远端是权威，如实转述退出码与 stderr 摘录。
			res.Verdict = VerdictUnreachable
			res.Reason = fmt.Sprintf("远端执行失败（退出码 %d）: %s", ex.ExitStatus(),
				clamp(strings.TrimSpace(firstNonEmpty(stderr.String(), stdout.String())), 160))
			return res, nil
		}
		res.Verdict, res.Reason = classifySession(runErr, ctx, p.Password, phaseDeadline)
		return res, nil
	}

	verdict, path, found := scanMarkers(stdout.Bytes())
	res.AuthorizedKeysPath = path
	if !found {
		res.Verdict = VerdictUnreachable
		res.Reason = "远端执行「成功」却没回预期标记，公钥状态未知，不冒充成功。输出摘录: " +
			clamp(strings.TrimSpace(firstNonEmpty(stdout.String(), stderr.String())), 160)
		return res, nil
	}
	res.Verdict = verdict
	res.OK = true
	switch verdict {
	case VerdictAppended:
		res.Reason = fmt.Sprintf("公钥已写入 %s", res.AuthorizedKeysPath)
	case VerdictAlreadyPresent:
		res.Reason = fmt.Sprintf("公钥已在 %s 中，远端未做改动（幂等）", res.AuthorizedKeysPath)
	}
	return res, nil
}

// ---------------------------
// 本地验货（第 2 条：验不过一个字节都不外发）
// ---------------------------

// loadPubKeyLine 读并校验公钥，返回 (单行公钥, SHA256 指纹, 拒发理由)。
// 拒发理由非空即 refused，调用方不得外发任何字节。
func loadPubKeyLine(path string) (line, fingerprint, refusal string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "公钥文件读取失败: " + err.Error()
	}
	if len(raw) == 0 {
		return "", "", "公钥文件为空"
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", "", "公钥文件只有空白字符"
	}
	if len(raw) > maxPubFileBytes {
		return "", "", fmt.Sprintf("公钥文件 %d 字节，远超合法公钥形态，拒发", len(raw))
	}
	trimmed := strings.TrimRight(string(raw), "\r\n")
	if trimmed == "" {
		return "", "", "公钥文件只有空白字符"
	}
	// 多行 = 注入载体（换行后跟 shell 片段是最典型的动手脚形态），一律拒。
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", "", "公钥文件含换行（authorized_keys 是单行一钥格式），疑遭篡改，拒发"
	}
	// 控制字符同样不进命令通道（tab 允许——注释字段合法成员）。
	for i := 0; i < len(trimmed); i++ {
		if b := trimmed[i]; (b < 0x20 && b != '\t') || b == 0x7f {
			return "", "", fmt.Sprintf("公钥行含控制字符 0x%02x，拒发", b)
		}
	}
	key, _, options, rest, pErr := ssh.ParseAuthorizedKey([]byte(trimmed))
	if pErr != nil {
		return "", "", "不是合法的 authorized_keys 单行公钥（" + pErr.Error() + "），拒发"
	}
	// options 是比注入更危险的一类"多余字段"：command="..."/restrict/from= 这类
	// authorized_keys 前缀会原样落进远端，此后**每次用这把钥匙登录都以该用户权限执行
	// 指定命令**——被动手脚的 pub 文件用它造后门，比 shell 注入更安静。本能力部署的
	// 是裸公钥，带任何 option 一律拒发。
	if len(options) > 0 {
		return "", "", "公钥行带 authorized_keys options（" + strings.Join(options, ",") +
			"），本能力只部署裸公钥，拒发"
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return "", "", "公钥行尾部有多余内容，拒发"
	}
	// 指纹由本地从解析结果算——远端不回传公钥内容，也就没有"远端替我们算指纹"的口子。
	return trimmed, ssh.FingerprintSHA256(key), ""
}

// targetAddr 校验主机栏与端口，返回可直接拨号、可作钉库键的 addr（IPv6 已补方括号）。
// 判据与父包 sshprobe 刻意保持一致的措辞；子包不 import 父包（循环），故本地重实现。
func targetAddr(rawHost string, port int) (string, error) {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return "", errors.New("请填写主机地址（IP 或域名）")
	}
	if strings.Contains(host, "://") || strings.Contains(host, "@") {
		return "", fmt.Errorf("主机栏只填 IP 或域名，协议与端口有独立栏（当前为 %q）", host)
	}
	if i := strings.Index(host, "]"); i >= 0 && i != len(host)-1 {
		return "", fmt.Errorf("主机栏不要带端口（当前为 %q）——端口在右侧独立栏", host)
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", fmt.Errorf("主机栏只填 IP 或域名，端口在右侧独立栏（当前为 %q）", host)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("端口需为 1~65535 的整数（当前 %d）", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// ---------------------------
// 结论归类
// ---------------------------

// classifyDialError 拨号层失败：连不上与被掐预算分开报，用户要做的事不一样。
func classifyDialError(err error) (string, string) {
	if isDeadlineErr(err) {
		return VerdictTimeout, "连接超时（端口未在预算内应答）"
	}
	if errors.Is(err, context.Canceled) {
		return VerdictUnreachable, "被取消"
	}
	return VerdictUnreachable, "连不上: " + err.Error()
}

// classifyHandshake 归类 KEX+认证段的失败。
//
// 顺序讲究：算法没谈拢（有独立类型）与"应答不是 SSH"（看对端实际吐了什么）都排在
// 通用 unreachable 之前；"密码不对"与"服务器不接受密码登录"在 x/crypto/ssh 的 API
// 层不可区分（它没导出"服务端支持哪些认证方式"的信息），别猜，两种可能都写进 Reason。
func classifyHandshake(err error, recorded []byte, password string) (string, string) {
	var nego *ssh.AlgorithmNegotiationError
	if errors.As(err, &nego) {
		return VerdictUnreachable, "对方是 SSH 服务，但算法没谈拢（双方没有共同算法）"
	}
	if ev, ok := notSSHLine(recorded); ok {
		return VerdictNotSSH, fmt.Sprintf("端口通，但应答不像 SSH（首行 %q），认证未进行", ev)
	}
	if isDeadlineErr(err) {
		return VerdictTimeout, "超时（KEX 或认证未在预算内完成）"
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return VerdictAuthRejected, "这组凭据被拒绝。也可能是该服务器不接受密码登录（例如只允许密钥）——" +
			"x/crypto/ssh 未导出可区分两者的信息：" + redactSecret(err.Error(), password)
	}
	if errors.Is(err, io.EOF) {
		return VerdictUnreachable, "对端未开始 SSH 会话就断开了连接"
	}
	return VerdictUnreachable, "SSH 会话没谈成: " + redactSecret(err.Error(), password)
}

// classifySession 归类执行段失败。先看 ctx（取消会表现为"连接已关"类错误，
// 根因是取消，必须如实报，不能让 deadline 判断抢跑）；再看预算；剩下是远端行为。
//
// deadline 参数不是装饰：底层 conn 到期时 x/crypto 会拆掉 transport，错误传到
// session 层常被化妆成 ExitMissingError/"closed"而不是 net 超时——错误恰好在
// 预算线上或之后到达的，归因给超时这个真凶。
func classifySession(err error, ctx context.Context, password string, deadline time.Time) (string, string) {
	if cErr := ctx.Err(); cErr != nil {
		if errors.Is(cErr, context.DeadlineExceeded) {
			return VerdictTimeout, "超时（任务预算耗尽）"
		}
		return VerdictUnreachable, "被取消"
	}
	if isDeadlineErr(err) || (!deadline.IsZero() && !time.Now().Before(deadline)) {
		return VerdictTimeout, "超时（会话执行未在预算内完成）"
	}
	return VerdictUnreachable, "会话执行异常: " + redactSecret(err.Error(), password)
}

// isDeadlineErr 与父包同判据：net 超时、os 打码超时、ctx 超时都算。
func isDeadlineErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
}

// notSSHLine 在我记录的对端前 4KiB 字节里找"第一条有效识别行"，
// 判它像不像 SSH（口径同 RFC 4253：'-' 开头的行忽略）。x/crypto 对前言行很宽容
// （不是 SSH- 开头的先跳过再说），这份宽容恰是"端口上坐着别的协议"需要独立成态的原因。
func notSSHLine(buf []byte) (string, bool) {
	lines := strings.Split(string(buf), "\n")
	if len(lines) > 64 {
		lines = lines[:64]
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		switch {
		case line == "" || strings.HasPrefix(line, "-"):
			continue
		case strings.HasPrefix(line, "SSH-"):
			return "", false
		default:
			return clamp(line, 80), true
		}
	}
	return "", false
}

// scanMarkers 在远端 stdout 里找我们的标记（整行前缀匹配，后随实际路径）。
func scanMarkers(out []byte) (verdict, path string, found bool) {
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimRight(raw, "\r")
		for _, m := range []struct {
			marker, verdict string
		}{{markerAdded, VerdictAppended}, {markerPresent, VerdictAlreadyPresent}} {
			if line == m.marker {
				return m.verdict, defaultKeysPath, true
			}
			if rest, ok := strings.CutPrefix(line, m.marker+" "); ok {
				p := strings.TrimSpace(rest)
				if p == "" {
					p = defaultKeysPath
				}
				return m.verdict, p, true
			}
		}
	}
	return "", "", false
}

// ---------------------------
// 工具
// ---------------------------

// recordingConn 旁路记录对端发来的前 recordCap 字节（只读方向）。
// 记录不是为了改写协议行为——恰恰相反，握手照旧全权交给 x/crypto——
// 只是为了在失败收口时还能回答"那个端口上到底在说什么话"。
type recordingConn struct {
	net.Conn
	mu   sync.Mutex
	head []byte
}

func (c *recordingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.mu.Lock()
		if len(c.head) < recordCap {
			take := min(n, recordCap-len(c.head))
			c.head = append(c.head, b[:take]...)
		}
		c.mu.Unlock()
	}
	return n, err
}

func (c *recordingConn) snapshot() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.head...)
}

// redactSecret 与父包同口径：原文与两种转义形态都打码。
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

func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
