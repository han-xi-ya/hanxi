// Package instance 实现 PicLite 单实例运行引擎：
//
// PicLite 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 PicLite
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 与 ccswitch/papertodo 引擎的适配差异（上游实证结论，勿回退照抄）：
//   - 窗口唤起契约相同：tauri-plugin-single-instance 二次无参拉起经 WM_COPYDATA
//     转发主实例，回调无条件 show+focus 主窗口——"打开窗口"即信使拉起；
//   - 退出无任何优雅通道：上游 on_window_event 对主窗口 CloseRequested 一律
//     prevent_close+hide（关窗只藏进托盘），ExitRequested 未置 quitting 标志也被
//     prevent_exit 拦截，且没有 -quit 类 CLI。因此 Quit 直接 JobObject 强杀，
//     绝不向单实例消息窗口（-siw）投 WM_CLOSE——那只会 DestroyWindow 拆掉
//     单实例协议载体、既不退出又让后续信使失联（源码实证：DefWindowProc 默认行为）；
//     PicLite 配置为前端修改即写盘（app-profile.json），进程级终止不丢设置；
//   - 主窗口可见性不能借用 -siw 窗口判断：插件创建它时恒置 WS_VISIBLE
//     （靠 LAYERED 扩展样式隐形），IsWindowVisible 永远为 true——ccswitch 模板
//     该探针依赖窗口形态，对 PicLite 失真。改为按 PID EnumWindows 找可见的
//     正常顶层窗口（悬浮结果/监测等任何用户面窗口出现都视为"在用"，空闲退避更保守）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// PicLiteProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type PicLiteProbe interface {
	// IsRunning 命名互斥体存在性探测（单实例插件持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 指定 PID 的实例是否有可见用户窗口——空闲自动退出的豁免信号。
	// pid 为 0（external/未启动）时实现方应直接返回 false。
	IsMainWindowOpen(pid uint32) bool
}
