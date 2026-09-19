// Package instance 实现 FlClash 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 FlClash 领域适配：
//   - 进程快照枚举探针（CreateToolhelp32Snapshot 命中 FlClash.exe；上游契约
//     lib/common/lock.dart 实证单实例 = %APPDATA% 数据目录下 lock 文件的
//     RandomAccessFile.lock，无命名互斥体可 OpenMutex，与 markeron/ccswitch 的
//     互斥体探测不同——探针实现见 prober/probe_windows；Inspect 适配器附带
//     存活 PID，external 快照据此充实，supervisor.Probe 形状适配见 instance.go）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 唤窗 Win32 直操作（EnumWindows 按 PID → ShowWindow(SW_RESTORE) +
//     SetForegroundWindow，自有/外部实例通用）与 WM_CLOSE 优雅退出钩子
//     （内核 Stop grace 窗口内按 PID 投递）——信使二次拉起在 FlClash 上游
//     只触发文件锁单实例检查后 exit(0)、不唤窗（与 markeron 信使语义根本不同，
//     论证见 messenger.go），两条路都留在本包、不进内核。
//
// FlClash 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 FlClash
// 进程树，杜绝孤儿驻留。
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
