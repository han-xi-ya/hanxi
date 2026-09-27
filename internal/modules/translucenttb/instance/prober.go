// Package instance 实现 TranslucentTB 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 TranslucentTB 领域适配：
//   - 裸 GUID 命名互斥体探针（上游 constants.hpp MUTEX_GUID，非 identifier-sim；
//     supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - 打包实例归属收编与外部强制结束（adoption.go）：镜像落 WindowsApps 包部署位
//     的 external 快照收编显示托管并走三段退出链；真外部保留 external，
//     ForceQuitExternal 通道按进程身份复核强制结束（系统权限决定成败）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），异常退出/手动停止文案按本模块
//     旧词表逐字还原；
//   - 无 OpenWindow（信使语义重定义）：TranslucentTB 的设置 UI 全在托盘 XAML
//     飞控、无主窗口可唤，二次拉起"信使"的真实语义是通知运行实例 ResetState
//     （重设任务栏动态状态）+ 弹"已在运行"气泡，映射为独立操作（见 messenger.go）；
//   - WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内投递托盘消息窗口，上游
//     Exit() 保存 settings.json 后自退，超时 JobObject 强杀兜底）。
//
// TranslucentTB 由内核启动后绑定 Windows Job Object；默认 Detached
// （全托管统一口径）：Hanxi 退出/崩溃不连带，工具独立驻留托盘。
// 常驻特效工具禁用空闲自动退出（退出=任务栏特效消失，与"后台常驻"诉求正相反）——
// 本模块不实现宿主侧空闲退出巡检。
//
// 本包零框架依赖，便于单元测试。
package instance

import (
	"time"

	"hanxi/internal/platform"
)

// exeName 上游主程序文件名（external 属主校验与归属判定共用；与 version 子包
// exeName 同值，instance 不 import version——保持两子包零依赖分层）。
const exeName = "TranslucentTB.exe"

// TBProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type TBProbe interface {
	// IsRunning 命名互斥体存在性探测（上游单实例锁持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// FindInstances 枚举全部在跑的 TranslucentTB 映像实例（按启动时刻升序，
	// 最早的主实例在前；短命信使可能在列尾，消费方按归属判据取用）。
	// 返回项携带 PID/完整镜像路径/启动时刻——external 快照充实与强制退出
	// 身份复核共用此事实源；枚举失败回空切片（探测语义宁缺毋滥，不报错）。
	FindInstances() []platform.ProcInfo
}
