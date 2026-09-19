// Package instance 实现 MangoDisk 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 MangoDisk 领域适配：
//   - Tauri 单实例探针（命名互斥体 + 隐藏信号窗口按 PID 归属，见 prober/probe_windows）：
//     Inspect 上报带 PID 的 ProcInfo，external/自有按 PID 区分是内核原生语义；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - "窗口唤起"信使（短命二次拉起经单实例插件回调 show+focus，见 messenger.go）与
//     WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内经信号窗口 PID 投递）：
//     MangoDisk 无进程内 CLI，唯一外部契约即 tauri-plugin-single-instance。
//
// MangoDisk 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 MangoDisk
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// MangoDiskProbe 抽象单实例互斥体和 signal window PID 探测。
type MangoDiskProbe interface {
	// IsRunning 通过 Tauri 单实例互斥体判断是否存在任一 MangoDisk 主实例（不区分归属）。
	IsRunning() bool
	// WaitForReady 轮询直至互斥体与信号窗口同时出现（GUI 完全就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// SignalPID 读取 Tauri 隐藏信号窗口的属主 PID，用于区分外部实例与自有实例。
	SignalPID() (uint32, bool)
}
