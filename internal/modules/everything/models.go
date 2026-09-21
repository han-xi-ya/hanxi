package everything

// ControlOutcome 控制操作（启动后台 / 打开窗口）执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started-background / started-window / opened / external-opened / external-running / already-running / busy
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 目标实例已终止（自有，或按 N3 分档处置成功的外部实例）
	External bool   `json:"external"` // true = 本轮面对的是外部自行启动的实例
	Forced   bool   `json:"forced"`   // true = 走了强杀通道（外部实例优雅失效后的兜底）
	Method   string `json:"method"`   // 结果归因（graceful-request/forced/blocked-elevated/...，见 externalquit）
	Message  string `json:"message"`  // 面向用户的执行说明
}

// DownloadTicket 本模块统一下载进度事件载荷（"everything:download"）。
// Component 区分两类资产：app = Everything 主程序版本包；es = ES 搜索组件。
type DownloadTicket struct {
	Component string `json:"component"` // app / es
	Version   string `json:"version"`   // 目标版本（es 组件为固定版本号常量）
	Stage     string `json:"stage"`     // downloading / verify / extract / done / error
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	Message   string `json:"message"`
}
