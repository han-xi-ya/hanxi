package instance

import (
	"time"

	"hanxi/internal/platform"
)

// WindowClass Everything 主实例始终创建的托盘通知窗口类名（1.4/1.5 相同通道名）。
// ES.exe 经该类窗口的 IPC 通道向主实例发查询——是"已有 Everything 实例在运行"的权威信号。
const WindowClass = "EVERYTHING_TASKBAR_NOTIFICATION"

// SearchWindowClass Everything 搜索主窗口的类名（跨 1.4/1.5 稳定，官方窗口消息协议的
// 已知锚点）。用于空闲自动退出的豁免判定：窗口开着说明用户可能在用。
const SearchWindowClass = "EVERYTHING"

// MutexName Everything 默认命名实例的单实例互斥体名（1.4/1.5 相同，-instance 定制实例除外）。
// 作为窗口探测的兜底第二通道（窗口类名若随版本变动，互斥体仍可判活）。
const MutexName = "EVERYTHING"

// EverythingProbe 探测 Everything 实例存在性（接口注入，便于单元测试替换为 fake）。
type EverythingProbe interface {
	// IsEverythingRunning 窗口类或互斥体任一存在即视为运行中
	IsEverythingRunning() bool
	// WaitForEverythingReady 阻塞轮询直至实例出现或超时（100ms 间隔）
	WaitForEverythingReady(timeout time.Duration) bool
	// IsSearchWindowOpen 搜索主窗口是否打开（空闲自动退出的豁免信号）
	IsSearchWindowOpen() bool
	// FindInstance 补充归属事实：按进程名枚举 Everything.exe 并查询路径/启动
	// 时刻（W2/N2 起供 external 快照充实与分档退出令牌）。拿不到（权限/竞态）
	// 返回 nil，不影响"在运行"判定本身。
	FindInstance() *platform.ProcInfo
}
