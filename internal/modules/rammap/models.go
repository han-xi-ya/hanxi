package rammap

// ControlOutcome OpenWindow 编排回执（多实例：聚焦自有/唤回外部/冷启动由后端裁决）。
type ControlOutcome struct {
	Action   string `json:"action"` // started / focused / external-focused / starting / external-unreachable
	External bool   `json:"external"`
	Message  string `json:"message"`
}

// QuitOutcome 退出请求结果（自有实例或按 N3 force-free 档处置的外部实例）。
type QuitOutcome struct {
	Stopped        bool   `json:"stopped"`
	Forced         bool   `json:"forced"`
	CloseRequested bool   `json:"closeRequested"`
	Method         string `json:"method"`
	External       bool   `json:"external"`
	Message        string `json:"message"`
}

// StatusInfo 前端状态投影：引擎快照 + 提权预告（载荷 manifest 强制管理员，
// stopped 态引导行需如实提示——提权三重契约之③，非提权 Hanxi 会撞 740）。
// 直接复用 instance.Snapshot 作事件载荷，本结构仅承载 GetStatus 附加位。
type StatusInfo struct {
	RequiresElevation bool `json:"requiresElevation"` // 恒 true：上游 manifest 事实
	HostElevated      bool `json:"hostElevated"`      // 当前 Hanxi 是否已提权（决定能否直接启动）
}
