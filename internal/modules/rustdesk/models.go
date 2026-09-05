package rustdesk

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（唤自有窗口）/ spawned-new（驻留托盘派生新窗）/ external-opened（唤外部实例窗口）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
