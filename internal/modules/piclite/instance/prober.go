// Package instance 实现 PicLite 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 PicLite 领域适配：
//   - 单实例互斥体探针（supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，收口后由终态广播纠正）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - "窗口唤起"信使（短命二次拉起经 WM_COPYDATA 转发，见 messenger.go）：
//     PicLite 无进程内 CLI，唯一外部契约即 tauri-plugin-single-instance——
//     第二次无参拉起 exe 经插件回调无条件 show+focus 主窗口；
//   - 退出无任何优雅通道：QuitHook 恒即时返错（见 instance.go），内核据此跳过
//     grace 窗口直接 JobObject 强杀。上游把"关窗"与"退出"做成两回事（主窗口
//     CloseRequested 一律 prevent_close+hide 驻托盘，ExitRequested 未置 quitting
//     标志也被 prevent_exit 拦截，且无 -quit 类 CLI）；绝不向单实例消息窗口
//     （-siw）投 WM_CLOSE——那只会 DestroyWindow 拆掉单实例协议载体、既不退出
//     又让后续信使失联（源码实证：DefWindowProc 默认行为）。PicLite 配置为前端
//     修改即写盘（app-profile.json），进程级终止不丢设置；
//   - 主窗口可见性不能借用 -siw 窗口判断：插件创建它时恒置 WS_VISIBLE
//     （靠 LAYERED 扩展样式隐形），IsWindowVisible 永远为 true——ccswitch 模板
//     该探针依赖窗口形态，对 PicLite 失真。改为按 PID EnumWindows 找可见的
//     正常顶层窗口（悬浮结果/监测等任何用户面窗口出现都视为"在用"，空闲退避更保守）；
//   - 外部实例检测用单实例插件的命名互斥体（{identifier}-sim），与 ccswitch 同构。
//
// PicLite 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 PicLite
// 及其 WebView2 子进程树，杜绝孤儿驻留。
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
