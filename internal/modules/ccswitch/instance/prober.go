// Package instance 实现 CC Switch 单实例运行引擎：
//
// CC Switch 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// HubKit 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 CC Switch
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 与 markeron/everything 引擎的适配差异：
//   - CC Switch 无进程内 CLI，唯一外部契约是 tauri-plugin-single-instance：
//     第二次无参拉起 exe 经 WM_COPYDATA 转发给主实例，由回调无条件 show+focus
//     主窗口——这就是"打开窗口"的可靠实现（markeron 想有而没有的能力）；
//   - 退出无 CLI 通道：向单实例消息窗口投递 WM_CLOSE 尽力优雅
//     （按用户托盘设置退出或驻留），宽限后 JobObject 强杀兜底；
//   - 外部实例检测用单实例插件的命名互斥体（identifier-sim），与 markeron 同构。
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
