package markeron

// ToggleOutcome 标注开关执行结果（"开关"语义：告知发生了什么动作，而非状态快照——
// 标注态本身是奇偶推断，用户直接按全局快捷键会脱同步，故不对外承诺精确状态）。
type ToggleOutcome struct {
	Outcome  string `json:"outcome"` // started（冷启动，仅运行未标注）/ toggled（自有实例翻转）/ external-toggled（外部实例翻转）
	Drawing  bool   `json:"drawing"` // 翻转后引擎推算的标注态（仅自有实例可信）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// StopOutcome 停止执行结果。
type StopOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
