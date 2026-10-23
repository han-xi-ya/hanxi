package sshkeys

// SSH 凭据测试：完成密钥交换，再用给定用户名/密码试一次密码认证。
//
// 为什么与"版本测试"分成两个能力、两个按钮：这一步会**真的向服务端发起一次登录尝试**，
// 在对端 sshd 日志里留下 Failed password 记录，也可能触发 fail2ban 之类的封禁动作。
// 它必须是显式动作，不能藏在"看看端口通不通"后面顺手做掉。
//
// 也正因为动了凭据，这里的口径比代理那侧更严：密码只在这一次调用里存在，不进结果
// 字段、不进错误文本（一律过 redactSecret 打码）、不进配置。

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHAuthVerdict 是凭据测试的结论，互斥。别让一个 bool 同时表达"连上了"和"能登录"
// 两件事——它们在只允许密钥登录的机器上会给出完全相反的含义。
type SSHAuthVerdict string

const (
	// SSHAuthLoginOK 用户名密码可用。这批结论里唯一能称为"能登录"的一态。
	SSHAuthLoginOK SSHAuthVerdict = "login-ok"
	// SSHAuthWrongCreds 这次认证被拒。x/crypto/ssh **没导出**能表达"服务端支持哪些
	// 认证方式"的类型，所以"密码不对"与"这台机器压根不接受密码登录"（比如只允许密钥）
	// 在 API 层面是同一个错误——与其猜一个，不如把两种可能都写进 Reason。
	SSHAuthWrongCreds SSHAuthVerdict = "wrong-credentials"
	// SSHAuthUnreachable 连不上；或连上了但 SSH 会话谈不成（端口被非 SSH 服务占着、
	// 算法协商失败——后者有独立类型，如实分开报）。
	SSHAuthUnreachable SSHAuthVerdict = "unreachable"
	// SSHAuthTimeout 预算耗尽。KEX 与认证共用同一条 conn deadline。
	SSHAuthTimeout SSHAuthVerdict = "timeout"
)

// SSHAuthRequest 凭据测试入参。
type SSHAuthRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	// Pass 只用于这一次认证，不回显、不落盘。
	Pass      string `json:"pass"`
	TimeoutMs int    `json:"timeoutMs"`
}

// SSHAuthResult 凭据测试结果。
//
// **没有 password 字段**，错误文本一律过 redactSecret——结果要进前端、要被复制。
//
// 这里**不报服务端版本横幅**：x/crypto/ssh 的 HostKeyCallback 是
// `(hostname, remote, key) error` 这个签名，拿不到 ConnMetadata，也就拿不到版本。
// 想读横幅用同页的「连接测试」（它专做那一件事）；不为此再补一次连接，把两个来源
// 混进同一个字段。
type SSHAuthResult struct {
	Addr     string         `json:"addr"`
	Verdict  SSHAuthVerdict `json:"verdict"`
	CanLogin bool           `json:"canLogin"`
	Reason   string         `json:"reason,omitempty"`
	// 主机密钥指纹在 KEX 阶段就拿到了，**认证失败也照样保留**："这台机器是不是它"
	// 与"这组密码对不对"是两个问题，后者失败不该把前者的证据一起丢掉。
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"`
	HostKeyType        string `json:"hostKeyType,omitempty"`
	LatencyMs          int64  `json:"latencyMs"`
}

// CheckSSHAuth 用给定凭据试一次 SSH 密码认证。error 通道只装"没能开测"（参数非法）。
func (p *prober) CheckSSHAuth(ctx context.Context, req SSHAuthRequest) (*SSHAuthResult, error) {
	addr, err := sshTargetAddr(req.Host, req.Port)
	if err != nil {
		return nil, err
	}
	user := strings.TrimSpace(req.User)
	if user == "" {
		return nil, errors.New("请填写用户名")
	}
	// 空密码单独立闸：有些服务端真接受空密码，但更常见的是直接拒——那时用户会看到
	// "密码被拒绝"，而实际情况是他压根没输密码。宁可在入口就说清。
	if req.Pass == "" {
		return nil, errors.New("请填写密码（本能力只测密码认证；空密码会得出误导性的「被拒绝」结论）")
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if req.TimeoutMs <= 0 {
		timeout = SSHCheckDefaultTimeoutMs * time.Millisecond
	} else if req.TimeoutMs > 30000 {
		timeout = 30 * time.Second
	}

	res := &SSHAuthResult{Addr: addr}
	start := time.Now()
	defer func() { res.LatencyMs = time.Since(start).Milliseconds() }()

	// 凭据放进 cfg.Auth：x/crypto/ssh 把认证做在 NewClientConn 内部，没有独立的
	// client.Auth 步骤（与不少 SSH 客户端库的用法不同，容易记错）。
	var captured struct{ keyType, fingerprint string }
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(req.Pass)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured.keyType = key.Type()
			captured.fingerprint = ssh.FingerprintSHA256(key)
			// **不做已知主机校验**：这是探测工具，手上没有对方的 known_hosts；直接拒
			// 反而让人看不到"对端换了 host key"这个最该被发现的信号。指纹原样给出，
			// 由人去跟自己的 known_hosts 对。
			return nil
		},
		Timeout: timeout,
	}

	dialer := &net.Dialer{Timeout: timeout}
	netConn, dErr := dialer.DialContext(ctx, "tcp", addr)
	if dErr != nil {
		res.Verdict = SSHAuthUnreachable
		res.Reason = classifySSHError(dErr)
		return res, nil
	}
	// 这套 API 不吃 ctx，KEX 与认证都靠这条 deadline 兜住（认证复用同一个底层 conn，
	// 一次设到位即可）。
	_ = netConn.SetDeadline(time.Now().Add(timeout))

	sshConn, chans, reqs, connErr := ssh.NewClientConn(netConn, addr, cfg)
	res.HostKeyFingerprint = captured.fingerprint
	res.HostKeyType = captured.keyType
	if connErr != nil {
		_ = netConn.Close()
		res.Verdict, res.Reason = classifyAuthError(connErr, req.Pass)
		return res, nil
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	res.Verdict = SSHAuthLoginOK
	res.CanLogin = true
	res.Reason = "用户名密码可用"
	return res, nil
}

// classifyAuthError 把 NewClientConn 的失败分成三态。
//
// 认证失败与 KEX 失败都从同一个调用返回，而 x/crypto/ssh 没有导出区分两者的类型；
// 认证被拒的可靠特征是错误里的 "unable to authenticate"（client_auth.go 的稳定措辞）。
// 例外是算法协商：它有独立类型 *ssh.AlgorithmNegotiationError（指针接收者），必须单独
// 认出来——"对方是 SSH 但双方没有共同算法"与凭据毫无关系，混进"密码被拒"就是骗人。
func classifyAuthError(err error, secret string) (SSHAuthVerdict, string) {
	if isDeadlineErr(err) {
		return SSHAuthTimeout, "超时（KEX 或认证未在预算内完成）"
	}
	var nego *ssh.AlgorithmNegotiationError
	if errors.As(err, &nego) {
		return SSHAuthUnreachable, "对方是 SSH 服务，但算法没谈拢（双方没有共同算法）"
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return SSHAuthWrongCreds, "这组凭据被拒绝。也可能是该服务器不接受密码登录（例如只允许密钥）——" +
			"x/crypto/ssh 未导出可区分两者的信息：" + redactSecret(err.Error(), secret)
	}
	return SSHAuthUnreachable, "SSH 会话没谈成: " + redactSecret(err.Error(), secret)
}

func isDeadlineErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
}
