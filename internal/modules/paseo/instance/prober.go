// Package instance 实现 Paseo 桌面端单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 Paseo 领域适配：
//   - 进程名 Paseo.exe + EnumWindows 探针（supervisor.Probe 形状适配，探针实现
//     见 prober/probe_windows；单实例锁按 userData 派生、无可依赖的命名互斥体，
//     进程名+窗口是唯一可靠信号，recordly 同族）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 唤窗双通道（优先 Win32 直唤 FocusWindow，信使 OpenMessenger 仅兜底——
//     上游 second-instance 语义是 openAdditional 新开非聚焦，决策见 service
//     .OpenWindow 注释）与 WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内投递）
//     ：不属进程治理，不经内核 Engine；
//   - externalSettle 树级静默期：进程名探测覆盖整棵进程树，wait 退出分类探针
//     前静默 500ms 防"自己刚退"误判（仅受管在途探针，静止态校正保持即时）。
//
// Paseo（Electron 应用，coding agent 编排器）由内核启动后绑定 Windows
// Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE，无 breakaway 许可），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 Paseo
// 主进程、其内置 daemon（ELECTRON_RUN_AS_NODE 子进程，同为 Paseo.exe 镜像）
// 以及 daemon 拉起的 agent/PTY 子进程树——整树终止归 Job，杜绝孤儿驻留。
//
// 上游契约（packages/desktop/src/main.ts 源码实证）：
//   - app.requestSingleInstanceLock() 按 **user-data 目录** 分实例组；Paseo 无
//     便携数据激活器，Electron 数据恒在 %APPDATA%\Paseo（集成拍板：托管实例与
//     用户自装实例共享数据、同锁互斥），故全局至多一个桌面主实例——
//     自有与外部天然互斥，wait 退出分类的外部接管判定因此可靠；
//   - Windows 下无托盘常驻：window-all-closed → app.quit()（main.ts 实证），
//     before-quit 走 daemon 清理生命周期——Quit 以 WM_CLOSE 尽力优雅，
//     宽限期取 recordly 的 3s 之上（daemon/PTY 收敛更重），超时 JobObject 强杀兜底。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// PaseoProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type PaseoProbe interface {
	// IsRunning 是否存在 Paseo.exe 进程（不区分自有/外部实例；锁互斥保证
	// 全局至多一个桌面主实例）。
	IsRunning() bool
	// WaitForReady 轮询等待 Paseo 主窗口出现（可见且带标题），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsWindowOpen Paseo 是否有可见窗口（"打开窗口"优先直唤的依据）。
	IsWindowOpen() bool
	// FocusWindow 将任一可见带标题的 Paseo.exe 顶层窗口恢复并置前台；
	// 成功返回 true。无窗可唤返回 false（此时才允许退到二次拉起）。
	FocusWindow() bool
}
