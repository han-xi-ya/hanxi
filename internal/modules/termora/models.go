package termora

// ControlOutcome OpenWindow 编排回执（单实例信使/冷启动分支由后端裁决，UI 恒一钮）。
type ControlOutcome struct {
	Action   string `json:"action"`   // started / focused / external-focused / starting
	External bool   `json:"external"` // true = 本轮面对的是外部自行启动的实例
	Message  string `json:"message"`
}

// QuitOutcome 退出请求结果（自有实例或按 N3 分档处置的外部实例）。
// Action="confirm-required" 表示 confirm-force 档首入未获授权，前端弹全局
// 确认框后携 confirm=true 重入（vscode 安装闸同款往返契约）。
type QuitOutcome struct {
	Stopped        bool   `json:"stopped"`
	Forced         bool   `json:"forced"`
	CloseRequested bool   `json:"closeRequested"`
	Method         string `json:"method"`
	External       bool   `json:"external"`
	Action         string `json:"action,omitempty"`
	Message        string `json:"message"`
	// Risk 非空 = 需要用户知情确认的风险文案（confirm-required 时送达 UI）。
	Risk string `json:"risk,omitempty"`
}
