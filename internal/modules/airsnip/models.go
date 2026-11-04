package airsnip

// ControlOutcome 控制类动作（打开设置面板 / 截图 / 贴图）的执行结果说明。
//
// Action 词表（前端主要按 message 呈现，action 供诊断与后续分流，不得随意增删）：
//   - started 冷启动后打开设置面板（一次点击走完启动+唤窗）
//   - opened  向已在场的实例请求打开设置面板
//   - shot    已投递截图命令（覆盖层是否真的弹出受 UIPI 与上游改版影响）
//   - pin     已投递"从剪贴板贴图"命令
//   - starting 前序启动仍在途，本次不做二次拉起
//
// 外部实例（用户自行启动、非 Hanxi 托管）不叠 action 前缀，由 External 标志承载。
type ControlOutcome struct {
	Action   string `json:"action"`
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
