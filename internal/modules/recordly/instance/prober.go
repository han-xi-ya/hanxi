// Package instance 实现 Recordly 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 Recordly 领域适配：
//   - 进程名 Recordly.exe + EnumWindows 探针（supervisor.Probe 形状适配，
//     探针实现见 prober/probe_windows；Electron 单实例锁按 userData 派生、
//     无可依赖的命名互斥体，进程名+窗口是唯一可靠信号，paseo 同族；Inspect
//     适配器承担受管在途探针的 externalSettle 树级静默期，见 instance.go）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 唤窗信使二次拉起（Electron second-instance 转发，不入 Job 不入治理，
//     见 messenger.go）与 WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内投递）；
//   - 托管环境变量注入通道：service 层经 StartOptions.Env 传入
//     RECORDLY_DISABLE_AUTO_UPDATES=1（内核 Spec.Env 受控字段落地，本包不再
//     自带 merge 实现）。
//
// Recordly（Electron 应用）由内核启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出，
// 内核都会连带终止 Recordly 及其渲染/GPU/原生采集子进程树（wgc-capture、
// cursor-monitor 等 helper 全部继承 Job），杜绝孤儿驻留。
//
// 上游契约（electron/main.ts 源码实证）：
//   - app.requestSingleInstanceLock()（打包态强制启用），拿不到锁直接 app.quit()
//     ——二次无参拉起即"信使"：主实例收到 second-instance 事件后
//     restoreWindowSafely（show + moveTop + focus）唤起窗口；
//   - 退出无 CLI 通道：向窗口投递 WM_CLOSE 尽力优雅（Electron 正常走
//     close 生命周期），宽限后 JobObject 强杀兜底；
//   - 上游内置 electron-updater 会自动覆写安装目录：托管必须注入
//     RECORDLY_DISABLE_AUTO_UPDATES=1 关闭（updater.ts 源码实证官方开关）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// RecordlyProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type RecordlyProbe interface {
	// IsRunning 是否存在 Recordly.exe 进程（不区分自有/外部实例）。
	IsRunning() bool
	// WaitForReady 轮询等待 Recordly 主窗口出现（可见且带标题），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen Recordly 是否有可见窗口——空闲自动退出的豁免信号
	//（录制 HUD 浮层在场同样命中：录制中绝不空闲退出）。
	IsMainWindowOpen() bool
}
