package snipaste

// LaunchOutcome 表示一次脱管启动请求的事实性结果。
type LaunchOutcome struct {
	Version string `json:"version"`
	Message string `json:"message"`
}

// QuitOutcome 表示页面手动退出实例的结果（自有实例或按 N3 分档处置的外部实例）。
type QuitOutcome struct {
	Stopped        bool   `json:"stopped"`
	Forced         bool   `json:"forced"`
	CloseRequested bool   `json:"closeRequested"`
	Method         string `json:"method"`
	External       bool   `json:"external"` // true = 本轮面对的是外部自行启动的实例
	Message        string `json:"message"`
}
