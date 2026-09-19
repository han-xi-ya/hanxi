// Package instance 实现 Keyviz 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别、
// PID 复用防护与 Detach 解除退出联动）收口至共享内核 hanxi/packages/go/supervisor；
// 本包只保留 Keyviz 领域适配：
//   - 单实例互斥体探针（supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，收口后由 stopped/failed 终态广播纠正）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移）；"已手动停止"折回本引擎的空文案、
//     内核异常退出文案改回 "Keyviz 异常退出（退出码 N）。请确认已安装
//     WebView2 Runtime"；
//   - QuitHook 恒即时返错（见 instance.go）：Keyviz 无优雅退出通道，内核据
//     supervisor 语义（钩子返错→直接强制终止）把 Quit 收敛为 JobObject 强杀。
//
// 与 ccswitch/piclite 引擎的适配差异（上游 lib.rs 实证结论，勿回退照抄）：
//   - 无窗口唤起能力：上游单实例回调是空函数 `|_, __, ___| {}`——二次无参拉起
//     只让信使进程静默退出（exit 0），不会 show+focus 任何窗口；设置窗口唯一
//     入口是托盘菜单（左键点击即弹）。故本引擎刻意无 OpenWindow/信使路径，
//     托管侧"打开设置"只能引导用户操作托盘，这是上游契约而非实现偷懒；
//   - 退出无任何优雅通道：上游 Quit 仅存在于托盘菜单回调（process::exit(0)），
//     无 -quit CLI、无命令管道；向单实例消息窗口（-siw）投 WM_CLOSE 是净损害
//     （DefWindowProc 直接 DestroyWindow，既不退出又拆掉单实例协议载体——
//     piclite 同款源码实证）；
//   - 数据安全：Keyviz 配置经 tauri-plugin-store 写 %APPDATA%\org.keyviz\store.json，
//     autoSave 节流 1s 且仅设置窗口修改时触发，进程级终止不丢历史设置；
//   - 无空闲自动退出：Keyviz 是"运行即在用"的全局按键可视化驻留工具，
//     不存在"关窗驻托盘闲置"形态，空闲语义不成立。
//
// Keyviz 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 Keyviz
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// KeyvizProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type KeyvizProbe interface {
	// IsRunning 命名互斥体存在性探测（单实例插件持有 = 主实例存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
