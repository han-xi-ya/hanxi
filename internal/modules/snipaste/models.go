package snipaste

// LaunchOutcome 表示一次脱管启动请求的事实性结果。
type LaunchOutcome struct {
	Version string `json:"version"`
	Message string `json:"message"`
}

// QuitOutcome 表示页面手动退出当前会话自有实例的结果。
type QuitOutcome struct {
	Stopped        bool   `json:"stopped"`
	Forced         bool   `json:"forced"`
	CloseRequested bool   `json:"closeRequested"`
	Method         string `json:"method"`
	Message        string `json:"message"`
}
