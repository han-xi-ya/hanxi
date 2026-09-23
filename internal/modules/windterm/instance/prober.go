// prober.go 实例探测契约（形状对齐 guoheview 多实例家族 + snipaste 的
// ProcInfo 充实要求——external 快照与退出令牌都要真实身份）。
//
// WindTerm 上游契约（2026-09-23 侦查实证，勿按单实例家族模板想当然）：
//   - 多实例应用：二进制全量扫描无 QtSingleApplication/qipsingle/单实例
//     互斥体痕迹（QLocalSocket 符号仅 Qt 网络模块导入表），二次无参拉起
//     得到独立新进程；探测一律走进程名 WindTerm.exe（Toolhelp32 快照）；
//   - Qt 窗口类名随版本漂移（Qt5XXQWindowIcon 家族），绝不按类名
//     FindWindow；唤窗/就绪判定按 "可见 + 带标题顶层窗口 + PID 归属"；
//   - 主程序 manifest 为 asInvoker（无 740 提权契约），但用户可能自行以
//     管理员运行——external 态按 N3 终裁 confirm-force 档经 externalquit
//     分档执行，提权目标 UIPI 拦截如实降级。
package instance

import (
	"context"
	"time"

	"hanxi/internal/platform"
)

// Probe WindTerm 实例探测接口（免框架依赖；service 层注入 Windows 实现，
// 单测以 fake 驱动）。
type Probe interface {
	// Inspect 报告是否存在归属非 ownPID 的 WindTerm.exe 进程及其首个实例
	// 身份（external 判定与退出令牌来源；ownPID=0 时任何进程都算）。
	// 全部候选 Query 失败时返回 (true, nil, nil)：在场但身份不明——
	// 调用方可判 external 存在，但不得据此强杀（保守回指引）。
	Inspect(ctx context.Context, ownPID uint32) (running bool, info *platform.ProcInfo, err error)
	// WaitForReady 轮询等待出现可见带标题的 WindTerm 顶层窗口，超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// FocusMainWindow 将指定 PID 的可见顶层窗口恢复（最小化）并置于前台，
	// 成功返回 true。pid 为 0 时实现方应直接返回 false。
	FocusMainWindow(pid uint32) bool
	// FocusAnyWindow 唤回任一 WindTerm.exe 的可见顶层窗口（外部实例聚焦）。
	// 多开窗口时命中枚举顺序第一个，属尽力而为。
	FocusAnyWindow() bool
}
