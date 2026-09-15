// Package instance 实现 Paseo 桌面端单实例运行引擎：
//
// Paseo（Electron 应用，coding agent 编排器）由本引擎启动后绑定 Windows
// Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出，
// 内核都会连带终止 Paseo 主进程、其内置 daemon（ELECTRON_RUN_AS_NODE 子进程，
// 同为 Paseo.exe 镜像）以及 daemon 拉起的 agent/PTY 子进程树，杜绝孤儿驻留。
//
// 上游契约（packages/desktop/src/main.ts 源码实证）：
//   - app.requestSingleInstanceLock() 按 **user-data 目录** 分实例组；Paseo 无
//     便携数据激活器，Electron 数据恒在 %APPDATA%\Paseo（集成拍板：托管实例与
//     用户自装实例共享数据、同锁互斥），故全局至多一个桌面主实例——
//     自有与外部天然互斥，wait() 的外部接管分类因此可靠；
//   - 单实例锁为 Chromium 内部对象（按 userData 派生），无可依赖的命名互斥体
//     ——探测一律走进程名 Paseo.exe + EnumWindows（recordly 同族）；
//   - second-instance 回调语义是 openAdditional **新开窗口而非聚焦**（main.ts
//     实证）——故唤窗优先 Win32 直操作（ShowWindow+SetForegroundWindow），
//     仅无窗可用时以二次拉起兜底请求开新窗（与 recordly 的信使唤窗不同族，
//     决策记录见 service.OpenWindow 注释）；
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
