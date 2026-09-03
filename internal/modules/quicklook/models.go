package quicklook

// ControlOutcome 启动操作的执行结果说明。
// QuickLook 无唤窗契约（托盘应用，设置入口在托盘菜单），控制动作只有启动语义。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ already-running（自有实例运行中）/ external-running（外部实例，未接管）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
