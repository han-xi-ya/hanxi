package rammap

// ControlOutcome OpenWindow 编排回执（多实例：聚焦自有/唤回外部/冷启动由后端裁决）。
type ControlOutcome struct {
	Action     string `json:"action"` // started / started-external-elevated / focused / external-focused / starting / external-unreachable
	External   bool   `json:"external"`
	Elevated   bool   `json:"elevated"`
	Managed    bool   `json:"managed"`
	CanQuit    bool   `json:"canQuit"`
	LaunchMode string `json:"launchMode"` // managed / external-elevated / external
	Message    string `json:"message"`
}

// LaunchMode 作为快照扩展的稳定词表，前端据此决定是否显示托管控制动作。
const (
	LaunchModeManaged          = "managed"
	LaunchModeExternal         = "external"
	LaunchModeExternalElevated = "external-elevated"
)

// QuitOutcome 退出请求结果（自有实例或按 N3 force-free 档处置的外部实例）。
type QuitOutcome struct {
	Stopped        bool   `json:"stopped"`
	Forced         bool   `json:"forced"`
	CloseRequested bool   `json:"closeRequested"`
	Method         string `json:"method"`
	External       bool   `json:"external"`
	Message        string `json:"message"`
}

// StatusInfo 前端状态投影：宿主提权态与目标 manifest 预判。
type StatusInfo struct {
	RequiresElevation bool   `json:"requiresElevation"`
	HostElevated      bool   `json:"hostElevated"`
	ExecutionLevel    string `json:"executionLevel"`
}
