// Package instance 实现 SubnetDesk 托管运行引擎：
//
// SubnetDesk（RustDesk 的 LAN 直连 fork）由本引擎启动后绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 无论以何种方式退出，内核都会连带
// 终止其整个进程树，杜绝孤儿驻留。
//
// 上游契约（rustdesk/rustdesk 与 zibo-chen/SubnetDesk 源码实证，两模块共用）：
//   - 发布 exe 是 rust-portable packer（libs/portable/src/main.rs）：运行后把内层
//     负载解压到 %LOCALAPPDATA%\{内层条目名小写}\（SubnetDesk 为 subnetdesk），
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
// 两形态纳管（便携隔离目录 + MSI 系统安装版）后，进程身份按"镜像路径可读且
// 命中提取目录/安装目录前缀"裁决：LocalSystem 服务与其派生 broker 因
// OpenProcess 失败被天然排除（真机实证），"服务常驻被误判为实例运行"不成立；
// JobObject 树杀亦只及客户端进程树，服务在 Job 外不受牵连——安装版形态下
// "Hanxi 退出"仅关客户端 UI，被控端经服务仍然可达（页面文案说明）。
// 代价：便携与安装两形态客户端并行时状态机同池计数，页面提示避免并行使用。
//
// 本包零框架依赖，便于单元测试。
package instance

// SubnetDeskProbe 实例存活与窗口探测（免框架依赖；service 层注入 Windows 实现）。
//
// 进程身份双来源（两形态纳管后）：便携提取目录（%LOCALAPPDATA%\subnetdesk）
// ∪ 安装版目录（注册表探测，如 C:\Program Files\SubnetDesk）。
type SubnetDeskProbe interface {
	// FindInstancePIDs 便携提取目录 + 安装版目录内全部进程 PID：
	// 含自有与外部实例（两形态），不含服务侧进程。
	FindInstancePIDs() []uint32
	// FindOwnPIDs 身份来源目录内、pid 命中 ancestors 或父链（可传递）命中
	// ancestors 任一成员的进程 PID。安装版直拉主进程时祖先本体即实例，须计入。
	FindOwnPIDs(ancestors []uint32) []uint32
	// IsRunning 两来源目录内存在任一客户端进程（自有或外部）。
	IsRunning() bool
	// HasVisibleWindow 给定进程集合中是否存在可见顶层窗口。
	HasVisibleWindow(pids []uint32) bool
	// FocusWindows 唤起给定进程的全部顶层窗口（SW_RESTORE + SetForegroundWindow），
	// 返回命中的窗口数（0 = 驻留托盘且窗口已销毁）。
	FocusWindows(pids []uint32) int
}
