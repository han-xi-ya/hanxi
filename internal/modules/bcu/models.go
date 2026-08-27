package bcu

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例唤窗）/ external-opened（外部实例唤窗）/ starting（启动中）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// DotnetEnv 本机 .NET 桌面运行时环境探测结果（框架依赖变体的推荐依据）。
type DotnetEnv struct {
	DesktopVersions []string `json:"desktopVersions"` // 已装 Microsoft.WindowsDesktop.App 版本（如 ["8.0.13"]）
	HasNet8         bool     `json:"hasNet8"`         // 是否装有 .NET 8 桌面运行时（BCU 6.x 目标框架）
}
