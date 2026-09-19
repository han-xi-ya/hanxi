// Package instance 实现 CC Switch 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 CC Switch 领域适配：
//   - 单实例互斥体探针（supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - "窗口唤起"信使（短命二次拉起经 WM_COPYDATA 转发，见 messenger.go）与
//     WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内投递）：CC Switch 无进程内
//     CLI，唯一外部契约即 tauri-plugin-single-instance——第二次无参拉起 exe 经
//     插件回调无条件 show+focus 主窗口（markeron 想有而没有的能力）；退出无 CLI
//     通道，WM_CLOSE 尽力优雅（按用户托盘设置退出或驻留），宽限后强杀兜底；
//   - 外部实例检测用单实例插件的命名互斥体（{identifier}-sim），与 markeron 同构。
//
// CC Switch 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 CC Switch
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// CCSwitchProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type CCSwitchProbe interface {
	// IsRunning 命名互斥体存在性探测（单实例插件持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 主窗口是否可见——空闲自动退出的豁免信号
	// （窗口开着说明用户可能正在其中操作；关窗驻托盘/纯托盘态返回 false）。
	IsMainWindowOpen() bool
}
