package portscan

// SSH 连接测试：拨过去，看它认不认自己是 SSH，并拿到对端版本横幅。
//
// 刻意只做版本交换（协议里的第一回合），不做密钥交换与认证——那需要
// golang.org/x/crypto/ssh，本仓当前不依赖它（go.mod 里连 x/crypto 都没有）。
// 因此这里的结论边界很明确：**能不能连、是不是 SSH、对端是什么实现**；
// "这个用户名密码对不对"不在此列，别把 OK 读成"能登录"。
//
// 之所以值得做：光 TCP 端口开着说明不了什么（端口撞车太常见），而一次 banner
// 交换就多花几十毫秒，换来的是"这确实是 22 端口上该有的东西 + 它是 OpenSSH 9.6
// 还是 Dropbear"——对排障（连不上到底是网络问题还是服务端问题）这是最有用的一跳。

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	// SSHCheckDefaultTimeoutMs 单次测试总预算（连接 + 读横幅）。SSH 的 banner 在服务端
	// accept 后立刻发出，正常情况百毫秒级；给 5s 是留给跨网/高延迟链路。
	SSHCheckDefaultTimeoutMs = 5000

	// sshBannerMaxBytes / sshBannerMaxLines：读上限。对端在识别行之前可以塞任意多的
	// 前言文本（RFC 4253 只规定"以 '-' 开头的行忽略"），不封顶就等于把内存交给对方。
	sshBannerMaxBytes = 4096
	sshBannerMaxLines = 64

	// sshClientIdent 我方版本行，协议要求以 CRLF 结束。comment 用 hanxi 实名：
	// 对端 sshd 日志里能看出是谁来过，伪装成 OpenSSH 只会污染人家的审计记录。
	sshClientIdent = "SSH-2.0-hanxi_1.0\r\n"
)

// SSHCheckRequest 一次 SSH 连接测试的入参。不含任何凭据——这一层用不上，也就不会泄露。
type SSHCheckRequest struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TimeoutMs int    `json:"timeoutMs"`
}

// SSHCheckResult SSH 连接测试结果。
type SSHCheckResult struct {
	Addr string `json:"addr"`
	// OK 的含义是"连上了，且对端按协议亮出了 SSH 身份横幅"。**不等于可登录**。
	OK            bool   `json:"ok"`
	ServerVersion string `json:"serverVersion,omitempty"` // 原始行，如 SSH-2.0-OpenSSH_9.6p1
	Protocol      string `json:"protocol,omitempty"`      // 2.0 / 1.99（兼容 SSH1 的旧实现）
	Software      string `json:"software,omitempty"`      // comment 段，如 OpenSSH_9.6p1 Ubuntu
	LatencyMs     int64  `json:"latencyMs"`
	Error         string `json:"error,omitempty"`
}

// CheckSSH 测一条 SSH 端点。error 通道只装"没能开测"（参数非法），
// 连不上/不是 SSH 都是正常结论（OK=false + Error 文本）。
func (s *Scanner) CheckSSH(ctx context.Context, req SSHCheckRequest) (*SSHCheckResult, error) {
	addr, err := sshTargetAddr(req.Host, req.Port)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if req.TimeoutMs <= 0 {
		timeout = SSHCheckDefaultTimeoutMs * time.Millisecond
	} else if req.TimeoutMs > 30000 {
		timeout = 30 * time.Second
	}

	res := &SSHCheckResult{Addr: addr}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		res.Error = classifySSHError(err)
		return res, nil
	}
	defer conn.Close()

	// 整条测试共用一个死线：连上之后对端不回话（透明转发盒、端口被别的服务占着）
	// 是最常见的悬挂形态，必须由读超时兜住，而不是等调用方取消。
	_ = conn.SetDeadline(time.Now().Add(timeout))

	start := time.Now()
	line, reason, err := readSSHBanner(conn)
	res.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	if reason != "" {
		res.Error = reason
		return res, nil
	}

	proto, software, pErr := parseSSHBanner(line)
	if pErr != nil {
		res.Error = pErr.Error()
		return res, nil
	}

	// 我方身份行：发不出去说明对端已经不认这条连接了，如实判失败。
	if _, wErr := io.WriteString(conn, sshClientIdent); wErr != nil {
		res.Error = "对端收下版本行前就断开了: " + wErr.Error()
		return res, nil
	}

	res.OK = true
	res.ServerVersion = line
	res.Protocol = proto
	res.Software = software
	return res, nil
}

// readSSHBanner 读到第一条**有效**识别行为止。
// 返回 (识别行, 判定文本, error)：三者分别对应"拿到了"、"拿到了但不是 SSH"、"没拿到"。
func readSSHBanner(conn net.Conn) (ident, notSSH string, err error) {
	br := bufio.NewReader(io.LimitReader(conn, sshBannerMaxBytes))
	for i := range sshBannerMaxLines {
		raw, rErr := br.ReadString('\n')
		line := strings.TrimRight(raw, "\r\n")
		if line != "" {
			switch {
			case strings.HasPrefix(line, "-"):
				// RFC 4253：识别行之前以 '-' 开头的行必须忽略（有些实现放 ASCII 前言）。
			case strings.HasPrefix(line, "SSH-"):
				return line, "", nil
			default:
				return "", fmt.Sprintf("端口通，但应答不像 SSH（首行 %q）", clamp(line, 80)), nil
			}
		}
		if rErr != nil {
			if i == 0 && errors.Is(rErr, io.EOF) {
				return "", "", errors.New("对端直接断开了连接，未返回版本横幅")
			}
			var netErr net.Error
			if errors.As(rErr, &netErr) && netErr.Timeout() {
				return "", "", errors.New("等待版本横幅超时（端口开着但没回 SSH 话）")
			}
			return "", "", fmt.Errorf("读取版本横幅失败: %w", rErr)
		}
	}
	return "", "", fmt.Errorf("识别行之前前言过多（超过 %d 行），按非 SSH 处理", sshBannerMaxLines)
}

// parseSSHBanner 拆 `SSH-<协议版本>-<comment>`。
func parseSSHBanner(line string) (proto, software string, err error) {
	rest := strings.TrimPrefix(line, "SSH-")
	proto, software, ok := strings.Cut(rest, "-")
	if proto == "" || !ok {
		return "", "", fmt.Errorf("版本横幅形态不合规范: %q", line)
	}
	return proto, software, nil
}

// classifySSHError 与代理测试同口径：连不上与超时分开记，前者是网络/服务问题，
// 后者往往意味着"端口在别处被吃掉或防火墙静默丢包"，用户要做的事不一样。
func classifySSHError(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "连接超时"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "连接超时"
	}
	if errors.Is(err, context.Canceled) {
		return "被取消"
	}
	return "连不上: " + err.Error()
}

func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sshTargetAddr 校验主机栏与端口，返回可直接拨号的 addr（IPv6 已按规范补好方括号）。
//
// 抽成函数是因为**版本测试与凭据测试共用同一套判据**：两处各写一份，早晚一份支持
// `[::1]` 一份不支持，用户在两个标签页里会得到不同的报错。
//
// 主机栏支持 IPv4 / IPv6 / 域名三种形态。判"塞了端口"不能用"含冒号"——IPv6 字面量
// 全是冒号，那样写会把 2408::1 这类地址一律拒在门外；真塞了端口的形态特征是 `]`
// 之后还有内容（`[::1]:22`），或者"含冒号又解析不成 IP"。
func sshTargetAddr(rawHost string, port int) (addr string, err error) {
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
	// 剥掉用户写的方括号，下面统一交给 net.JoinHostPort——留着会让 dial 收到
	// "[[::1]]:22" 这种废地址。
	host = strings.Trim(host, "[]")
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", fmt.Errorf("主机栏只填 IP 或域名，端口在右侧独立栏（当前为 %q）", host)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("端口需为 1~65535 的整数（当前 %d）", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
