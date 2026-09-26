package gonavi

import "hanxi/internal/modules/gonavi/instance"

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例唤窗）/ external-opened（外部实例唤窗）/ starting（启动临界区）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
// 如实口径：GoNavi 上游关窗时对未保存 SQL 草稿弹确认框，WM_CLOSE 可能被模态
// 框挂住——托管退出在宽限后以 JobObject 强杀兜底，未经确认的未保存草稿将丢失，
// 前端退出入口应在执行前预告（"若有未保存 SQL 草稿会弹确认，托管退出前请自行处理"）。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// GoNaviStatus GetStatus 复合投影：实例快照 + 使用版本的账本漂移信号
// （version 线 VerifyLedger 只读复查结果；mtime 闸控成本纪律见其注释）。
// 内嵌 Snapshot 经 JSON 展平，前端消费形状 = instance-state 事件载荷 +
// drifted/driftNote 两字段。
type GoNaviStatus struct {
	instance.Snapshot
	Drifted   bool   `json:"drifted"`   // 使用版本 exe 发生账外漂移（只报告不处置）
	DriftNote string `json:"driftNote"` // 漂移/无法比对的如实明细（空 = 未复查）
}
