// Package instance 实现 Recordly 单实例运行引擎：
//
// Recordly（Electron 应用）由本引擎启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出，
// 内核都会连带终止 Recordly 及其渲染/GPU/原生采集子进程树（wgc-capture、
// cursor-monitor 等 helper 全部继承 Job），杜绝孤儿驻留。
//
// 上游契约（electron/main.ts 源码实证）：
//   - app.requestSingleInstanceLock()（打包态强制启用），拿不到锁直接 app.quit()
//     ——二次无参拉起即"信使"：主实例收到 second-instance 事件后
//     restoreWindowSafely（show + moveTop + focus）唤起窗口；
//   - Electron 单实例锁的内部对象名不可预期（Chromium 进程单例按 userData
//     派生），不存在可依赖的命名互斥体——与 ccswitch（tauri 固定 {id}-sim）、
//     bcu（源码常量 Global\...）本质不同，探测一律走进程名 Recordly.exe +
//     EnumWindows 按进程名过滤（bcu 的按 PID 枚举同族方案）；
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
