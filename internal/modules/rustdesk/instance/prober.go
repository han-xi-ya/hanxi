// Package instance 实现 RustDesk 托管运行引擎：
//
// RustDesk（自托管优先的开源远程桌面）由本引擎启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出，内核都会连带
// 终止其整个进程树，杜绝孤儿驻留。
//
// 上游契约（rustdesk/rustdesk master 源码实证，与 subnetdesk 模块共用）：
//   - 发布 exe 是 rust-portable packer（libs/portable/src/main.rs）：运行后把内层
//     负载解压到 %LOCALAPPDATA%\{内层条目名小写}\（RustDesk 为 rustdesk），
//     spawn 内层后**立即退出且不 wait**（cmd.spawn() 后 main 直接返回）——
//     因此外层进程既不是生命周期锚点、退出码也不反映内层存亡，引擎锚定在
//     "提取目录内的自有进程树存活"（父 PID 闭包识别，见 FindOwnPIDs）；
//   - 无单实例互斥体：二次无参拉起 = 又一个主窗口实例（非唤窗信使），
//     "唤起窗口"只能 EnumWindows 按 PID 直接操作（litemonitor 同族方案）；
//   - 主窗口关闭 = 窗口销毁但内层进程驻留托盘（--tray/--server 同树存活），
//     上游没有唤回/退出 CLI（无 --quit）：重新开窗唯一途径是再次拉起 packer；
//     退出无优雅通道（WM_CLOSE 只会再次藏窗），Quit 直接 JobObject 终止整树
//     ——进行中的远程会话会被切断，属预期语义（托管端"停止服务"）；
//   - 子进程 --server/--tray 由内层 run_me 拉起（同 token CreateProcess，
//     继承 Job），树杀天然覆盖。
//
// 与外部安装的 RustDesk（Program Files\RustDesk）天然两清：探测只认提取目录内
// 的进程，安装版 UI/托盘与其 Windows 服务进程均不在计数范围（非提权场景
// SYSTEM 服务进程还额外被 OpenProcess 失败跳过），双重避免
// "服务常驻被误判为实例运行"；代价是托管便携版与安装版并行时互不感知，页面文案说明。
//
// 本包零框架依赖，便于单元测试。
package instance

// RustDeskProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
type RustDeskProbe interface {
	// FindInstancePIDs 提取目录（%LOCALAPPDATA%\rustdesk）内全部进程 PID：
	// 含自有与外部便携实例，不含安装版/服务。
	FindInstancePIDs() []uint32
	// FindOwnPIDs 提取目录内、父链（可传递）命中 ancestors 任一成员的进程 PID
	// ——即本引擎拉起的外层 packer 及其全部后代。ancestors 含派生开窗的外层 PID。
	FindOwnPIDs(ancestors []uint32) []uint32
	// IsRunning 提取目录内存在任一便携进程（自有或外部）。
	IsRunning() bool
	// HasVisibleWindow 给定进程集合中是否存在可见顶层窗口。
	HasVisibleWindow(pids []uint32) bool
	// FocusWindows 唤起给定进程的全部顶层窗口（SW_RESTORE + SetForegroundWindow），
	// 返回命中的窗口数（0 = 驻留托盘且窗口已销毁）。
	FocusWindows(pids []uint32) int
}
