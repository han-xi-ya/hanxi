// Package instance 实现 TranslucentTB 单实例运行引擎：
//
// TranslucentTB 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// 开启联动时 Hanxi 退出会连带终止实例，任务栏特效随进程注销自动还原。
// 默认 Detached（全托管统一口径）：Hanxi 退出/崩溃不连带，工具独立驻留托盘。
//
// 与 ccswitch 引擎的适配差异（上游契约侦查实证，勿按 tauri 模板脑补）：
//   - 单实例互斥体是裸 GUID 名（344635E9-…，main.cpp wil unique_mutex），非 identifier-sim；
//   - 无主设置窗口：全部设置 UI 在托盘 XAML 飞控（MainAppWindow 即托盘消息窗口），
//     故本引擎没有 OpenWindow——二次拉起"信使"的真实语义是通知运行实例
//     ResetState（重设任务栏动态状态）+ 弹"已在运行"气泡，等价托盘菜单
//     "Reset dynamic state"，映射为独立的 ResetState 操作；
//   - 退出契约干净：消息窗口 WM_CLOSE → Exit()（保存 settings.json + PostQuitMessage），
//     信使/驻托盘场景不存在"关窗只隐藏"歧义，宽限后 JobObject 强杀兜底；
//   - 常驻特效工具禁用空闲自动退出（退出=任务栏特效消失，与"后台常驻"诉求正相反）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// TBProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type TBProbe interface {
	// IsRunning 命名互斥体存在性探测（上游单实例锁持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
