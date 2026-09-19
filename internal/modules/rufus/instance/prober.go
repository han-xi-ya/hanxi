// Package instance 实现 Rufus 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 Rufus 领域适配：
//   - 互斥体 + 进程枚举探针（supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - WM_CLOSE 优雅退出经内核 SetQuitHook 投递（close_windows 领域函数留本包）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的账目（ExitCode/StoppedAt/提权指引
//     覆盖文案）拼回既有事件契约（前端与 wails 事件载荷零漂移）；
//   - 唤窗/拒杀纪律（restoreWindowByPID/elevateHint）等平台领域函数留在本包文件，
//     不经内核。
//
// Rufus（Win32 原生启动盘制作工具）由内核启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出（托盘退出/
// 崩溃/强杀），内核都会连带终止进程，杜绝孤儿驻留。
//
// 上游契约（src/rufus.c / src/net.c / src/settings.h 源码实证）：
//   - 单实例 = 固定命名互斥体 Global\Rufus（CreateMutexA(NULL, TRUE,
//     "Global/" APPLICATION_NAME)，进程终生持有）——与 LiteMonitor 的
//     路径派生名不同，名称稳定可预测，存在性探测以互斥体为权威；
//   - 第二实例抢锁失败弹 MB_SYSTEMMODAL"已在运行"错误框后退出——
//     **不仅无唤窗回调，二次拉起还会炸出一个需要手动确认的系统模态框**，
//     所以"打开窗口"绝不能走信使，必须 EnumWindows 按 PID 直操作窗口；
//   - 无 CLI 退出信使（-g/-i/-l/-f/-w/-x 全为启动参数）；对话框应用无托盘，
//     关窗即退进程 → Quit 走 WM_CLOSE + 宽限 + JobObject 强杀兜底；
//     ⚠ 镜像写入中收到关闭请求会弹确认对话框，宽限内无人确认则被强杀
//     （产出半成品盘）——前端常态展示"写入中勿退出"警示条；
//   - manifest requireAdministrator：未提权的 Hanxi 经 CreateProcess 拉起
//     会直接失败（ERROR_ELEVATION_REQUIRED 740，不弹 UAC），Start 错误文案
//     特判指向"以管理员身份运行 Hanxi"；
//   - 数据落盘：exe 同目录存在 rufus.ini（哪怕空文件）即全设置走 ini、
//     app_data_dir 锚定 exe 目录（便携零污染）——托管版本目录由本包
//     config.go 预置种子 ini，并顺带关闭上游内置更新检查
//     （UpdateCheckInterval = -1 实证可永久禁用后台检查，版本管理归 Hanxi）。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// RufusProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type RufusProbe interface {
	// FindPIDs 返回全部 Rufus 进程 PID（空 = 未运行；含外部实例，
	// 覆盖托管的 rufus.exe 与外部浏览器原名下载的 rufus-4.15p.exe 形态）。
	FindPIDs() []uint32
	// IsRunning 存活权威信号：Global\Rufus 互斥体存在 或 进程枚举命中。
	// 互斥体不依赖 exe 文件名（用户随意改名仍命中），进程枚举补 PID 信息。
	IsRunning() bool
	// WaitForReady 轮询等待实例就绪（进程出现），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// IsMainWindowOpen 这些进程中是否存在可见顶层窗口。
	IsMainWindowOpen(pids []uint32) bool
}
