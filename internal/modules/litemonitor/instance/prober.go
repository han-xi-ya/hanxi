// Package instance 实现 LiteMonitor 单实例运行引擎：
//
// LiteMonitor（C# WinForms 桌面/任务栏硬件监控）由本引擎启动后绑定
// Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 退出时
// 内核连带终止进程树（含其拉起的 Updater/FPS 子进程），杜绝孤儿驻留。
//
// 上游契约（Program.cs / MainForm_Transparent.cs / app.manifest 源码实证）：
//   - 单实例 = 命名互斥体，但名称**随安装路径派生**
//     （Global\LiteMonitor_SingleInstance_{exe 目录消毒}_Mutex）——外部实例
//     安装路径不可预知，互斥体探测不可行，改走进程快照枚举（LiteMonitor.exe），
//     与 FlClash 同策略；
//   - 第二实例抢到互斥体失败直接 return——**无唤窗回调**，二次启动不可
//     用作"打开窗口"；唤窗由本引擎直操作：EnumWindows 按 PID 定位顶层窗口 →
//     ShowWindow(SW_RESTORE) + SetForegroundWindow（等价其托盘双击语义）；
//   - 退出菜单 = form.Close()，无 FormClosing 拦截（OnFormClosed 仅清理）——
//     向主窗投递 WM_CLOSE 即优雅退出；驻留窗口被隐藏（HideMainForm）时
//     WM_CLOSE 仍可达，宽限后 JobObject 强杀兜底；
//   - manifest requireAdministrator：未提权的 Hanxi 经 CreateProcess 拉起
//     会直接失败（ERROR_ELEVATION_REQUIRED 740，不弹 UAC），Start 错误文案
//     特判指向"以管理员身份运行 Hanxi"；
//   - 无 CLI 退出通道；settings.json 随 exe 目录（便携），首启由本包
//     config.go 落 seed 关闭内置自动更新检查（托管版本由 Hanxi 接管）。
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
