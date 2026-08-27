// Package instance 实现 FlClash 单实例运行引擎：
//
// FlClash（Flutter 桌面代理客户端）由本引擎启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），HubKit 退出时内核连带终止进程树。
//
// 上游契约（lib/common/lock.dart + window.dart 源码实证）：
//   - 单实例 = 文件锁（%APPDATA%\应用数据目录下的 lock 文件经 RandomAccessFile.lock），
//     第二实例获取失败直接 exit(0)——**无唤窗行为**（与 BCU 的 SetForegroundWindow
//     信使不同，二次启动不可用作"打开窗口"）；
//   - 无命名互斥体 → 存活探测走进程快照枚举（FlClash.exe），与既有模块的
//     互斥体探测不同；
//   - 唤窗由本引擎直接执行：EnumWindows 按 PID 定位顶层窗口 →
//     ShowWindow(SW_RESTORE) + SetForegroundWindow，自有/外部实例通用；
//   - 无 CLI 退出：WM_CLOSE（Flutter 默认关窗即退；驻托盘由强杀兜底）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// FlClashProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type FlClashProbe interface {
	// FindPIDs 返回全部 FlClash.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning FlClash.exe 进程存在性探测。
	IsRunning() bool
	// WaitForReady 轮询等待进程出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 这些进程中是否存在可见顶层窗口——
	// 空闲自动退出的豁免信号（窗口开着说明用户可能正在用）。
	IsMainWindowOpen(pids []uint32) bool
}
