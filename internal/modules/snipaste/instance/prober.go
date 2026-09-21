// prober.go 外部实例探针契约（W2 外部实例治理，2026-09-20 排期）。
//
// 探针形状即 supervisor.Probe（Inspect 单一事实源）：引擎在静止态经内核
// RefreshExternal 调用，报告"系统里是否存在非本会话启动的 Snipaste"。
// 历史背景：本模块曾长期挂 noExternalProbe（恒"不在运行"），管理边界=本会话
// 进程树；N3 终裁（C+风险分档，见 docs/plans/PLAN_W1_DECISIONS §1）后接入
// 真实探测，状态词表新增 external 态。
package instance

import (
	"context"

	"hanxi/internal/platform"
)

// Probe 外部 Snipaste 实例探测（接口注入，单测以 fake 驱动；Windows 实现见
// probe_windows.go 的进程名快照枚举）。
type Probe interface {
	// Inspect 报告 Snipaste 是否在运行及其实例信息（PID/路径/启动时刻）。
	// 引擎只在静止态调用（running/starting/stopping 时探到的正是自家进程会
	// 误导状态机），返回值中的 ProcInfo 用于充实 external 快照与退出令牌。
	Inspect(ctx context.Context) (running bool, info *platform.ProcInfo, err error)
}
