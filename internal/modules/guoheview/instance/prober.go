// Package instance 实现果核看图（GuoheView）运行实例引擎：
//
// GuoheView 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// "随 Hanxi 退出"开启时，Hanxi 无论以何种方式退出，内核都会连带终止托管实例，
// 杜绝孤儿驻留；关闭联动则完全不影响（看图器的典型期望）。
//
// 上游契约（真机实测 3.2.7，勿按单实例家族模板想当然）：
//   - 多实例应用：二次无参拉起得到的是独立新窗口（两进程并存），
//     候选命名互斥体（GuoheView / MagicView / {id}-sim 等）OpenMutex 全部
//     ERROR_FILE_NOT_FOUND——不存在可依赖的单实例锁，探测一律走进程名
//     GuoheView.exe（Toolhelp32 快照，recordly/bcu 同族方案）；
//   - "打开窗口"没有信使语义：自有实例在跑 → 聚焦它的主窗口（EnumWindows
//     按自有 PID 过滤 → SW_RESTORE + SwitchToThisWindow）；没在跑 → 直接拉起
//     新托管实例。窗口类 UiCore_Window 是果核 core-ui 框架共享类名，
//     绝不按类名 FindWindow（多实例与兄弟应用全撞车）；
//   - 关窗即退：向任一可见主窗投递 WM_CLOSE，实例数 3 秒内归零（实测）——
//     Quit 走 WM_CLOSE + 宽限 + JobObject 强杀兜底（ccswitch 模板结构）；
//     与 piclite（关窗藏托盘、只能强杀）相反，优雅通道存在且有效；
//   - 无空闲自动退出：多实例模型下"托管进程活着 = 窗口开着 = 用户在看图"，
//     空闲退出只会打断浏览，无收益（piclite 的 3 分钟巡检在此语义不成立）；
//   - 自动更新：config.ini 的 [update] 仅有 min_check_interval 节流键、无官方
//     关闭开关（实测键表），内置 GuoheView-Updater.exe 只在用户应用内点击后
//     拉起。托管实例的子进程由 JobObject 继承兜底，更新弹窗在页面提示条
//     引导用户回 Hanxi 管理版本（不越权改写上游配置键语义）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// ViewProbe 实例存活探测接口（免框架依赖；service 层注入 Windows 实现）。
type ViewProbe interface {
	// IsRunning 是否存在 GuoheView.exe 进程（不区分自有/外部实例）。
	IsRunning() bool
	// WaitForReady 轮询等待出现可见带标题的 GuoheView 顶层窗口，超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// FocusMainWindow 将指定 PID 的可见顶层窗口恢复并置于前台，成功返回 true。
	// pid 为 0（非自有实例）时实现方应直接返回 false。
	FocusMainWindow(pid uint32) bool
	// FocusAnyWindow 唤回任一 GuoheView.exe 的可见顶层窗口（外部实例聚焦）。
	// 多开窗口时命中枚举顺序第一个，属尽力而为。
	FocusAnyWindow() bool
}
