// Package instance 实现 Bili23 Downloader 单实例运行引擎：
//
// Bili23 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 Bili23 及其
// FFmpeg 转码子进程树，杜绝孤儿驻留。
//
// 与 ccswitch 引擎的适配差异（上游是 Python/PySide6 而非 tauri，实例模型实证自 src/main.py）：
//   - 单实例契约：应用自建命名互斥体 B096F0C1-…（与 Inno AppMutex 同 GUID），
//     探测用 OpenMutex 与 ccswitch 同构；
//   - 唤窗：主实例监听 QLocalServer "bili23_downloader_single_instance"，
//     二次无参拉起 exe 写 activate 唤醒主窗口后自行退出——信使语义同 ccswitch；
//   - 退出（关键差异）：上游"关闭窗口"行为用户可配（EXIT/MINIMIZE/ALWAYS_ASK），
//     外部 WM_CLOSE 不保证进程退出；且作为下载器，JobObject 强杀会中断在途任务。
//     因此本引擎 Quit 不做强杀兜底，改为两阶段宽限 + 三态结果如实上报
//     （exited / hidden 收入托盘 / windowUp 弹窗询问中），强杀仅在用户显式点
//     「强制结束」（Stop）时执行；
//   - 不做空闲自动退出：下载器无法从外部感知任务活跃度，误杀代价高。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// Bili23Probe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type Bili23Probe interface {
	// IsRunning 命名互斥体存在性探测（主实例 CreateMutexW 持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// HasVisibleWindow 指定进程是否有可见顶层窗口（关窗询问对话框 / 主窗口 /
	// 已收入托盘的判别信号）。pid 为 0 时返回 false。
	HasVisibleWindow(pid uint32) bool
}
