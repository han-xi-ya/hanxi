package translucenttb

// ControlOutcome 启动/重设状态操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ already-running（自有实例）/ external-detected（外部实例已在运行）/ starting（启动竞态中）/ reset-sent（自有实例信使重设）/ external-reset（外部实例信使重设）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
