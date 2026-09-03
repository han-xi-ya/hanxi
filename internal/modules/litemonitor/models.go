package litemonitor

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例唤窗）/ external-opened（外部实例唤窗）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// RuntimeStatus 系统 .NET 桌面运行时探测结果。
// LiteMonitor 发布 zip 为框架依赖版（TargetFramework=net8.0-windows 且不含
// runtime DLL）——缺 .NET 8 桌面运行时则启动即弹系统错误对话框。
// 前端据此展示条件提示条（papertodo RuntimeStatus 同构）。
type RuntimeStatus struct {
	DesktopRuntimes []string `json:"desktopRuntimes"` // 已装 WindowsDesktop.App 版本（升序）
	HasDesktop8     bool     `json:"hasDesktop8"`     // 是否存在 LiteMonitor 所需的 .NET 8 桌面运行时
}
