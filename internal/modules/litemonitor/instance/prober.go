// Package instance 实现 LiteMonitor 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 LiteMonitor 领域适配：
//   - 进程快照枚举探针（CreateToolhelp32Snapshot 命中 LiteMonitor.exe）——上游
//     单实例互斥体名随安装路径派生（Global\LiteMonitor_SingleInstance_{exe 目录
//     消毒}_Mutex），外部实例安装路径不可预知、名称无法复现，OpenMutex 探测对
//     外部实例完全失效，进程名是唯一稳定标识（与 FlClash 同策略，探针实现见
//     prober/probe_windows；Inspect 适配器附带存活 PID，external 快照据此充实，
//     supervisor.Probe 形状适配见 instance.go）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 唤窗 Win32 直操作（EnumWindows 按 PID → ShowWindow(SW_RESTORE) +
//     SetForegroundWindow，自有/外部实例通用）与 WM_CLOSE 优雅退出钩子（内核
//     Stop grace 窗口内按 PID 投递）——上游第二实例抢互斥体失败静默 return、
//     无唤窗回调（Program.cs 实证），二次拉起信使路径不存在，两条路都留在
//     本包、不进内核；
//   - manifest requireAdministrator 特判：未提权 spawn 直得 740（不代弹 UAC），
//     文案改写在 Start wrapper 内完成（见 instance.go holdSpawnFail 的收口窗口）；
//   - 无 CLI 退出通道；settings.json 随 exe 目录（便携），首启由本包 config.go
//     在 spawn 前落 seed 关闭内置自动更新检查（托管版本由 Hanxi 接管，
//     PreStart 播种属模块策略、内核不做启动前干预）。
//
// LiteMonitor 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止进程树
// （含其拉起的 Updater/FPS 子进程），杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// LiteMonitorProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type LiteMonitorProbe interface {
	// FindPIDs 返回全部 LiteMonitor.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning LiteMonitor.exe 进程存在性探测。
	IsRunning() bool
	// WaitForReady 轮询等待进程出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 这些进程中是否存在可见顶层窗口。
	IsMainWindowOpen(pids []uint32) bool
}
