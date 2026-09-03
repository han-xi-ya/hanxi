// Package instance 实现 PaperTodo 单实例运行引擎：
//
// PaperTodo 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 PaperTodo，
// 杜绝孤儿驻留（"不随 Hanxi 关闭"开关经 SetAllowKillOnClose 解除联动）。
//
// 上游契约（src/App.xaml.cs + src/SingleInstanceHelper.cs 源码实证）：
//   - WPF 自建单实例协议：裸命名互斥体 "PaperTodo-SingleInstance-Mutex" 判主，
//     第二实例把自身命令行参数经命名管道 "PaperTodo-SingleInstance-Activate"
//     转发给主实例后立即退出——探测用 OpenMutex，与 ccswitch 的 tauri 模式同构；
//   - 命令词表（src/StartupCommand.cs）：show|open / hide / toggle /
//     new-todo|todo / new-note|note|paper / exit|quit，空参数默认 show。
//     因此"唤窗"与"优雅退出"都有官方通道：二次拉起携带 show / exit 即可，
//     退出优先走优雅命令（数据自动保存 + 快照备份），宽限后 JobObject 强杀兜底。
//
// 与 ccswitch 的适配差异：Quit 不发 WM_CLOSE 而是"信使携 exit 命令"；
// 无空闲自动退出——桌面便签是常驻环境型工具，纸片不该因"无人点"被收走。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// PaperProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type PaperProbe interface {
	// IsRunning 命名互斥体存在性探测（主实例持有 = 任一主实例存活，不区分归属）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
