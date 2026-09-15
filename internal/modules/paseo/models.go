package paseo

// ControlOutcome 打开窗口操作的执行结果说明。
// Paseo 唤窗分三档：直唤已有窗（focused）、无窗时二次拉起请求开新窗
// （messenger，上游 second-instance = openAdditional 语义）、冷启动（started）。
type ControlOutcome struct {
	Action   string `json:"action"` // started / focused / messenger / external-*（前缀 external- 表示命中的是外部自启实例）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
