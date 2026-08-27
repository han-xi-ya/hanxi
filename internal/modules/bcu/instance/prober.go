// Package instance 实现 BCU 单实例运行引擎：
//
// BCU（Bulk Crap Uninstaller，.NET 8 WinForms 自包含应用）由本引擎启动后绑定
// Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），HubKit 退出时内核
// 连带终止进程树，杜绝孤儿驻留。
//
// 上游契约（EntryPoint.cs 源码实证）：
//   - 命名互斥体 Global\BCU-singleinstance 作单实例锁；
//   - 二次启动 = 信使语义：第二实例查找主进程后 SetForegroundWindow(MainWindowHandle)
//     唤起主窗口（无窗口时才弹错误框）——"打开窗口"直接无参拉起即可；
//   - 无托盘、无 CLI 退出；WinForms 窗口类名不可预测，退出与窗口探测一律走
//     EnumWindows + 进程 PID 过滤，与 ccswitch 的固定类名方案不同。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// BCUProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type BCUProbe interface {
	// IsRunning 命名互斥体存在性探测（主实例持有 = 存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 指定进程是否存在可见顶层窗口——
	// 空闲自动退出的豁免信号（窗口开着说明用户可能正在用）。
	IsMainWindowOpen(pid uint32) bool
}
