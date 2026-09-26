// Package instance 实现 GoNavi 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 GoNavi 领域适配：
//   - 进程名探测（CreateToolhelp32Snapshot 命中 GoNavi.exe + EnumWindows 主窗判据）
//     ——阶段 0 实证：GoNavi **便携态无单实例互斥体**（MSI 态才有激活事件
//     Local\GoNavi-CDD6BF2F-…-activate，刻意不依赖），OpenMutex 路线不存在，
//     进程名是唯一稳定标识（与 litemonitor/recordly 同族，探针实现见
//     prober/probe_windows；Inspect 适配器附带存活 PID，external 快照据此充实，
//     supervisor.Probe 形状适配见 instance.go）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回事件契约
//     （前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 唤窗 Win32 直操作（EnumWindows 按 PID → 公共件唤回，自有/外部实例通用）
//     与 WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内按 PID 投递）——上游无
//     CLI 开关、便携态第二实例不接管而是并存，二次拉起"信使"路径不成立
//     （拉起即多开一个 GoNavi，与用户意图相反），此决策理由请勿在后续维护中
//     "好心"改回二次拉起（litemonitor 同款结论）；
//   - 关窗语义（上游实证）：GoNavi OnBeforeClose 对未保存 SQL 草稿会弹确认框，
//     WM_CLOSE 可能被模态对话框挂住——Quit = 优雅 WM_CLOSE + 宽限
//     closeGracePeriod + JobObject 强杀兜底，宽限期为包级变量（单测可压缩）；
//   - 提权 manifest 为 asInvoker（无三重契约），spawn 无 740 特判通道；
//     无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"（窗口 Title
//     固定 "GoNavi"，就绪判据 = 该标题可见窗在场，WebView2 初始化完成才有，
//     比"进程在场"更强）。
//
// GoNavi 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 GoNavi
// 及其 WebView2 子进程树（msedgewebview2.exe 全部继承 Job），杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// GoNaviProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type GoNaviProbe interface {
	// FindPIDs 返回全部 GoNavi.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning GoNavi.exe 进程存在性探测（不区分自有/外部实例）。
	IsRunning() bool
	// WaitForReady 轮询等待 "GoNavi" 标题主窗口出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 给定 PID 集合中是否存在可见且标题为 "GoNavi" 的主窗口
	//（非空标题的对话框/辅助窗不作就绪证据——判据与上游窗口契约对齐）。
	IsMainWindowOpen(pids []uint32) bool
}
