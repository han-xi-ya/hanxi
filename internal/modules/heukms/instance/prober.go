// Package instance 实现 HEU KMS Activator 的单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 HEU 领域适配：
//
//   - 进程快照枚举探针（CreateToolhelp32Snapshot 按 **前缀** HEU_KMS_Activator 命中）：
//     上游主程序文件名随版本漂移（实测 v64.0.0 为 HEU_KMS_Activator_v64.0.0.exe），
//     精确名匹配一升级就瞎，故判据取前缀；与 litemonitor/FlClash 同策略放弃
//     OpenMutex——上游闭源（仓库 master 只剩两份 PDF 与 19 字节 README）且代码段
//     经 UPX 压缩，互斥体名静态取证不可得，外部实例即便存在也无从复现名称。
//     探针给出存活 PID，external 快照据此充实（supervisor.Probe 形状适配见 instance.go）。
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回空文案；
//   - 唤窗 Win32 直操作（EnumWindows 按 PID → SW_RESTORE + 借权置前台）与 WM_CLOSE
//     优雅退出钩子（内核 Stop grace 窗口内按 PID 投递）。**"打开窗口"不走二次
//     拉起信使**：上游单实例契约无法实证（见上），二次拉起可能另开一个新实例而非
//     唤回原窗，按 PID 直操作对自有/外部实例通用、无此风险；
//   - manifest requireAdministrator 特判（PE 资源 MANIFEST/1/2052 实证
//     level="requireAdministrator" uiAccess="false"）：未提权 spawn 直得 740
//     且不代弹 UAC，文案改写在 Start wrapper 内完成（见 holdSpawnFail 收口窗口）；
//   - 零上游干预：不播种/不改写上游任何配置文件（HEU 的选项全在它自己界面里，
//     托管只负责启停与版本台账——与"不 fork 上游"的项目承诺一致）。
//
// HEU 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止进程树——
// 上游是否会派生子进程无法静态取证（闭源 + UPX 压段），Job 语义不依赖该事实，
// 有子进程则一并收口，无则等价于单进程终止。
// "随 Hanxi 一起关闭"开关由 service 层以 DetachFromJob 表达，默认独立存活。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// HeuProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type HeuProbe interface {
	// FindPIDs 返回全部 HEU 主程序进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning HEU 主进程存在性探测。
	IsRunning() bool
	// WaitForReady 轮询等待进程出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 这些进程中是否存在可唤顶层窗口（含最小化在场）。
	IsMainWindowOpen(pids []uint32) bool
}
