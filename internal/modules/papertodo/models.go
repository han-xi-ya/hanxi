package papertodo

// ControlOutcome 打开窗口/收拢纸片操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例唤窗）/ external-opened（外部实例唤窗）/ hidden（收拢纸片）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// RuntimeStatus 系统 .NET 桌面运行时探测结果（no-runtime 变体的可用性提示依据，
// 直读 Program Files\dotnet\shared 目录，不依赖 dotnet CLI）。
type RuntimeStatus struct {
	DesktopRuntimes []string `json:"desktopRuntimes"` // 已装 WindowsDesktop.App 版本（升序）
	HasDesktop10    bool     `json:"hasDesktop10"`    // 是否存在 no-runtime 变体所需的 .NET 10 桌面运行时
}
