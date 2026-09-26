// Package instance 实现 DBX 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 DBX 领域适配：
//   - 单实例互斥体探针（tauri-plugin-single-instance，identifier com.dbx.app →
//     命名互斥体 {identifier}-sim；semver feature 未启用、无版本后缀跨版本恒定；
//     supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 互斥体"拒探"分治（阶段 0 实证纪律）：OpenMutex 因完整性/ACL 被拒
//     （ERROR_ACCESS_DENIED）时互斥体结论不可得，IsRunning 兜底改走
//     进程名 DBX.exe 枚举 + EnumWindows 主窗判据（title 恒 "DBX"）；
//   - 隐藏消息窗口类 {identifier}-sic / 名 {identifier}-siw 是单实例插件的
//     转发窗，**任何就绪/主窗判据都不得当主窗枚举**（它常驻在场且不可见，
//     拿它探活会把引擎永久钉死在 running）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - "窗口唤起"信使（二次拉起触发 single-instance handoff，见 messenger.go）：
//     DBX 无任何唤窗 CLI 参数，唯一外部契约即插件回调的无条件 show+focus——
//     与托管"打开窗口"语义天然重合；信使进程 Start+Release 不 Wait 不进 Job；
//   - WM_CLOSE 优雅退出钩子（内核 Stop grace 窗口内按自有 PID 投递顶层窗）：
//     DBX 关窗语义为 CloseRequested → 驻托盘或弹询问确认框（无 CLI quit），
//     Quit = WM_CLOSE + 宽限 closeGracePeriod（包级变量）+ JobObject 强杀兜底；
//   - 数据目录裁决（阶段 0 定死）：托管启动注入 env DBX_DATA_DIR=<Hanxi 数据根
//     稳定路径>（上游实证 env 优先级最高；不注入则出厂 portable 数据随版本
//     目录走、删版本丢数据）——信使接管场合同源注入，托管生命周期内改道无洞；
//   - 提权 manifest 为 asInvoker（无三重契约），spawn 无 740 特判通道；
//     Detached 默认随家族（不随 Hanxi 关闭）；GUI 子系统程序零窗口干预（零 fork）。
//
// 特有坑（外部实例治理口径）：用户开启 DBX 自带"托管备份"会创建 schtasks
// 计划任务，以 --managed-backup-worker 身份再拉起 DBX.exe（脱出我们的
// JobObject）。Quit 后 RefreshExternal 若检见同名残留如实呈现 external，指引
// 文案由 service 层给出——Hanxi **绝不强杀托管之外的 DBX.exe 进程**。
//
// DBX 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 DBX
// 及其 WebView2 子进程树，杜绝孤儿驻留。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// DBXProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type DBXProbe interface {
	// IsRunning 存活探测：互斥体结论可得的场以互斥体为准（单实例插件持有
	// = 主实例存活）；互斥体拒探（ERROR_ACCESS_DENIED）兜底进程名 + 主窗判据。
	IsRunning() bool
	// WaitForReady 轮询等待存活判据成立（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 是否存在可见且标题为 "DBX" 的顶层主窗（判据刻意不碰
	// 单实例插件的隐藏消息窗 -sic/-siw——那是转发信使窗，不是主窗）。
	IsMainWindowOpen() bool
	// FindPIDs 返回全部 DBX.exe 进程 PID（含外部实例与托管备份 worker 残留；
	// WebView2 子进程为 msedgewebview2.exe 异名，不会污染计数）。
	FindPIDs() []uint32
}
