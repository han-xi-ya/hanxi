// prober.go 实例探测契约（形状对齐 windterm/snipaste 家族——Inspect 单一
// 事实源 + 窗口级聚焦；就绪判据换用单实例互斥体）。
//
// Termora 上游契约（2026-09-23 源码实证 Application.kt/ApplicationSingleton.kt，
// 勿按多实例家族模板想当然）：
//   - 单实例应用：Windows 下 CreateMutex("termora")（会话级命名互斥体），
//     二次无参拉起 = 向持锁实例发激活 tick 后自行退出（信使语义，与
//     ccswitch/everything 同族）——托管"打开窗口"走信使而非 EnumWindows 广播；
//   - 进程身份探测按 exe 名 Termora.exe（jpackage 启动器；jvm 在进程内，
//     不产生同名子进程，pty 子进程 winpty/conpty 异名不扰探测）；
//   - Java AWT 顶层窗口类名恒 SunAwtFrame（不随版本漂移），但多窗口/隐藏
//     工具窗并存，聚焦仍按"PID 归属 + 可见 + 带标题"收敛，绝不按类名广播；
//   - 主启动器 manifest asInvoker（无 740 提权契约）；用户自行提权运行会
//     产生 UIPI 拦截态——external 退出按 N3 终裁 confirm-force 档，blocked
//     如实降级（externalquit 执行器收口）。
package instance

import (
	"context"
	"time"

	"hanxi/internal/platform"
)

// Probe Termora 实例探测接口（免框架依赖；service 层注入 Windows 实现，
// 单测以 fake 驱动）。
type Probe interface {
	// Inspect 报告是否存在归属非 ownPID 的 Termora.exe 进程及其身份
	// （external 判定与退出令牌来源；ownPID=0 时任何进程都算）。
	// 全部候选 Query 失败时返回 (true, nil, nil)：在场但身份不明——
	// 调用方可判 external 存在，但不得据此强杀（保守回指引）。
	Inspect(ctx context.Context, ownPID uint32) (running bool, info *platform.ProcInfo, err error)
	// MutexHeld 单实例互斥体 "termora" 是否在世（就绪判定快路径，ccswitch 同法）。
	MutexHeld() bool
	// WaitForReady 等待实例就绪（互斥体在位），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// FocusMainWindow 将指定 PID 的可见顶层窗口恢复（最小化）并置于前台，
	// 成功返回 true。pid 为 0 时实现方应直接返回 false。
	FocusMainWindow(pid uint32) bool
	// FocusAnyWindow 唤回任一 Termora.exe 的可见顶层窗口（外部实例聚焦兜底）。
	FocusAnyWindow() bool
}
