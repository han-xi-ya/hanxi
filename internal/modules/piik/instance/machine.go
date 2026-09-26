package instance

import (
	"fmt"
	"strings"
)

// 上游机读模式（env PIIK_CLIENT_GATE_NO_BROWSER=true）stdout 打点前缀
// （阶段 0 实锤的四条机读字段 + 一处命中位），逐行解析进引擎账目：
//
//	Local access: open                     本地访问免口令可用
//	Local access password: X               口令已设（X 为明文——解析即弃，
//	                                       引擎账只记"口令已设"布尔）
//	LAN invitation origin: http://…        局域网邀请来源
//	Public invitation origin: http://…     公网（Cloudflare 隧道）邀请来源
//
// 任意含 GATE_NO_BROWSER 字样的行置 noBrowser（上游"自动拉浏览器被闸/失败"
// 的告知位；本引擎恒注入该 env，置位即事实"界面需手动打开"，前端据此亮
// 兜底链接）。错误全走 stderr，不进本机读解析面（只进日志）。
const (
	machineAccessPrefix   = "Local access: "
	machinePasswordPrefix = "Local access password: "
	machineLanPrefix      = "LAN invitation origin: "
	machinePublicPrefix   = "Public invitation origin: "
	machineNoBrowserMark  = "GATE_NO_BROWSER"
)

// machineUpdate 一行 stdout 对引擎机读账目的一次改写（纯函数、值语义，
// 单测钉死解析面；未命中任何机读字段时 changed=false）。
//
// 记账语义（重要）：零值 ≠ 未命中——每个布尔载荷都配 hit 位，未命中的
// 字段绝不参与账目改写（否则一条 "Local access password: X" 的零值
// localOpen 会抹掉先前 "Local access: open" 的入账，字段间互相踩踏）。
type machineUpdate struct {
	changed          bool
	accessHit        bool   // "Local access: " 行命中（localOpen 载荷有效）
	localOpen        bool   // 按取值是否 "open" 置位
	passwordHit      bool   // 口令行命中（passwordSet 载荷有效）
	passwordSet      bool   // 值非空（明文在此结构性丢弃，不落任何账）
	lanInvitation    string // 非空即改写
	publicInvitation string // 非空即改写
	noBrowser        bool   // 仅置位向（true 才有意义）
}

// parseMachineLine 解析单行 stdout 机读打点。口令行前缀先于 "Local access: "
// 判定（两前缀共享头部字符串但互不包含，次序为纵深防御）；行尾 \r
// （CRLF 泵送残留）统一剥除。
func parseMachineLine(line string) machineUpdate {
	line = strings.TrimRight(line, "\r")
	var u machineUpdate

	switch {
	case strings.HasPrefix(line, machinePasswordPrefix):
		u.passwordHit = true
		u.passwordSet = strings.TrimSpace(strings.TrimPrefix(line, machinePasswordPrefix)) != ""
		u.changed = true
	case strings.HasPrefix(line, machineAccessPrefix):
		u.accessHit = true
		u.localOpen = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, machineAccessPrefix)), "open")
		u.changed = true
	case strings.HasPrefix(line, machineLanPrefix):
		u.lanInvitation = strings.TrimSpace(strings.TrimPrefix(line, machineLanPrefix))
		u.changed = true
	case strings.HasPrefix(line, machinePublicPrefix):
		u.publicInvitation = strings.TrimSpace(strings.TrimPrefix(line, machinePublicPrefix))
		u.changed = true
	case strings.Contains(line, machineNoBrowserMark):
		u.noBrowser = true
		u.changed = true
	}
	return u
}

// maskSecretLine stdout 日志通道脱敏：口令行的明文在进环形缓冲/OnLog 事件
// 之前就地置换为 ***（快照面本就不带明文，日志面同样不得带出——事件载荷
// 即前端可见，秘密值无处可逃才是真纪律，frpc Redact 同源思想）。
func maskSecretLine(line string) string {
	if strings.HasPrefix(line, machinePasswordPrefix) {
		return machinePasswordPrefix + "***"
	}
	return line
}

// URLForPort piik 本机界面地址单点组装（引擎只提供 URL，浏览器调用归
// service 层——"唤窗"语义在无窗服务上的等价物）。
func URLForPort(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}
