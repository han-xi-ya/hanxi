package sshkeys

import (
	"net/url"
	"strings"
)

// prober 是 SSH 主动探测的载体类型。
//
// 为什么不是沿用 portscan 的 Scanner：Scanner 是端口扫描引擎本体（ExecuteScan 全套
// 家当都挂在它上面），跟着 SSH 探测一起搬走等于把"扫描"也搬了。这里只留一个空壳，
// 目的是保持搬家前的调用形状（newProber().CheckSSH(...)），让搬过来的两份测试**正文
// 一行都不必改**——那些用例（密码不得出现在结果任何字段、认证失败也保留主机指纹、
// "只接受公钥"与"密码不对"在 API 层不可区分所以两种都写进 Reason、端口通但不是 SSH /
// 回话静默 / 连上就断 四种形态分开报）是本次搬迁最值钱的资产，重写一遍只会写得更弱。
//
// 与 Scanner 一样无状态，所有方法可并发调用。
type prober struct{}

// newProber 创建探针。构造无 IO、无副作用。
func newProber() *prober { return &prober{} }

// redactSecret 把文本里可能带着的凭据三种形态抹成 ***。
//
// 定位是**纵深防御**，不是"已知会泄露"：x/crypto/ssh 本身不回显口令，但
// CheckSSHAuth 的 Reason 字段拼的是认证失败的原始错误文本，而那段文本的构成
// 不完全由我们控制（服务端回显、库实现变动都可能把它带进来）；结果要进前端、
// 会被用户复制。所以出口处统一抹净，**不指望日志层兜底**——logging.Redact 只打码
// `password=` 这类赋值式写法，盖不住原值与 URL 转义形态。
//
// 三种形态都试：原值、QueryEscape、PathEscape。空串跳过，否则 ReplaceAll(msg, "", "***")
// 会把整段文本炸开。
//
// 与 portscan/proxyprobe.go 的同名函数同源（那边为代理 URL 的 userinfo 泄露而设，
// 是真实泄露路径；这边是纵深防御，两者动机不同但算法一致）。
//
// 刻意复制、不抽公共层：遵循机主既有裁决「重复家族保持独立、可各自拆走，勿主动
// 抽取」。代价如实写在这儿——**这是安全函数，改一处可能忘别处**；现存同形件共
// 三份（portscan/proxyprobe.go、本文件、本子模块 deploy/deploy.go，后者注释自认
// "与父包同口径"），"第三处再抽"的触发条件已具备，是否动手归机主裁。若将来判定
// 它无用要删，须同步 sshauth.go 的两处调用与 TestCheckSSHAuthNeverLeaksPassword
// 那条锁。
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
