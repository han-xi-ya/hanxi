// Package platform 定义操作系统能力的抽象接口与跨平台数据模型（网卡、端口表、
// 进程、Job Object、应用包等），不含任何系统调用实现。具体实现位于
// platform/windows（Windows 专属）；业务模块只依赖本包接口，便于非 Windows 构建降级。
package platform

import (
	"context"
	"errors"
	"time"

	"hanxi/internal/platform/apppackage"
)

// 通用平台错误定义
var (
	ErrNotSupported     = errors.New("platform: operation not supported")
	ErrProcessNotFound  = errors.New("platform: process not found")
	ErrTokenMismatch    = errors.New("platform: process token mismatch (possible pid reuse)")
	ErrProtectedProcess = errors.New("platform: process is protected (system redline)")
	ErrAccessDenied     = errors.New("platform: access denied (elevation required)")
)

// IP 协议族
type Family int

const (
	FamilyIPv4 Family = 4
	FamilyIPv6 Family = 6
)

// TCP 连接状态
type TCPState string

const (
	TCPStateClosed      TCPState = "CLOSED"
	TCPStateListen      TCPState = "LISTEN"
	TCPStateSynSent     TCPState = "SYN_SENT"
	TCPStateSynReceived TCPState = "SYN_RCVD"
	TCPStateEstablished TCPState = "ESTABLISHED"
	TCPStateFinWait1    TCPState = "FIN_WAIT_1"
	TCPStateFinWait2    TCPState = "FIN_WAIT_2"
	TCPStateCloseWait   TCPState = "CLOSE_WAIT"
	TCPStateClosing     TCPState = "CLOSING"
	TCPStateLastAck     TCPState = "LAST_ACK"
	TCPStateTimeWait    TCPState = "TIME_WAIT"
	TCPStateDeleteTCB   TCPState = "DELETE_TCB"
	TCPStateUnknown     TCPState = "UNKNOWN"
)

// IPv6Detail 详细 IPv6 地址（公网、临时、链路本地）
type IPv6Detail struct {
	Address     string `json:"address"`
	Type        string `json:"type"` // "Public" (公网/主地址) | "Temporary" (临时隐私地址) | "LinkLocal" (链路本地)
	IsTemporary bool   `json:"isTemporary"`
}

// Adapter 网卡信息
type Adapter struct {
	Index       uint32       `json:"index"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	MAC         string       `json:"mac"`
	IPv4        []string     `json:"ipv4"`
	IPv6        []string     `json:"ipv6"`
	IPv6Details []IPv6Detail `json:"ipv6Details"`
	Gateway     string       `json:"gateway"`
	IPv6Gateway string       `json:"ipv6Gateway"`
	DNSServers  []string     `json:"dnsServers"`
	IsPhysical  bool         `json:"isPhysical"`
	IsLoopback  bool         `json:"isLoopback"`
	IsUp        bool         `json:"isUp"`
}

// Neighbor 邻居表/ARP 缓存项
type Neighbor struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Interface uint32 `json:"interface"`
	State     string `json:"state"` // Reachable, Stale, etc.
}

// TCPRow 对应系统 TCP 表行
type TCPRow struct {
	LocalIP    string   `json:"localIp"`
	LocalPort  uint16   `json:"localPort"`
	RemoteIP   string   `json:"remoteIp"`
	RemotePort uint16   `json:"remotePort"`
	State      TCPState `json:"state"`
	PID        uint32   `json:"pid"`
}

// UDPRow 对应系统 UDP 表行
type UDPRow struct {
	LocalIP   string `json:"localIp"`
	LocalPort uint16 `json:"localPort"`
	PID       uint32 `json:"pid"`
}

// ProcInfo 进程详细信息
type ProcInfo struct {
	PID       uint32    `json:"pid"`
	Name      string    `json:"name"`
	ExePath   string    `json:"exePath"`
	StartedAt time.Time `json:"startedAt"`
	Owner     string    `json:"owner"`
}

// VerifyToken 查杀复核令牌（防止 PID 快速复用误杀）
type VerifyToken struct {
	PID       uint32    `json:"pid"`
	ExePath   string    `json:"exePath"`
	StartedAt time.Time `json:"startedAt"`
}

// Job Windows Job Object 句柄封装接口
type Job interface {
	// Assign 把已存在的进程（按 PID，需 PROCESS_QUERY_LIMITED_INFORMATION 打开句柄）纳入本 Job。
	Assign(pid uint32) error
	// Close 关闭 Job 句柄；若 KILL_ON_JOB_CLOSE 生效则连带终止组内所有进程。
	Close() error
	// Terminate 立即以指定退出码杀死 Job 内全部进程，句柄本身仍需 Close。
	Terminate(exitCode uint32) error
	// SetAllowKillOnClose 动态调整 KILL_ON_JOB_CLOSE 限制：
	// true（默认，创建即启用）= Hanxi 退出/崩溃时内核连带杀 Job 内进程；
	// false = 工具独立运行，Hanxi 退出完全不影响它（"不随 Hanxi 关闭"开关）。
	SetAllowKillOnClose(enabled bool) error
}

// NetworkAPI 网络与接口抽象
type NetworkAPI interface {
	// Adapters 枚举全部网卡（含回环与虚拟网卡，由调用方按 IsLoopback/IsPhysical 过滤）。
	Adapters() ([]Adapter, error)
	// DefaultAdapter 返回承载默认路由的网卡；无活动出网网卡时返回错误。
	DefaultAdapter() (*Adapter, error)
	// NeighborTable 返回邻居/ARP 缓存，供局域网发现去重与在线判断。
	NeighborTable() ([]Neighbor, error)
	// Ping 探测目标可达性并返回往返时延。ok=false 且 err=nil 表示正常超时不可达；
	// err 非空仅用于系统级失败（权限/构造 ICMP 句柄出错等），ctx 取消也走 err。
	Ping(ctx context.Context, ip string, timeout time.Duration) (rtt time.Duration, ok bool, err error)
}

// PortAPI 端口与连接表抽象
type PortAPI interface {
	// TCPTable 按协议族快照系统 TCP 连接表（一次调用即一致性视图，无分页）。
	TCPTable(family Family) ([]TCPRow, error)
	// UDPTable 按协议族快照系统 UDP 监听表。
	UDPTable(family Family) ([]UDPRow, error)
}

// ProcessAPI 进程管理与安全查杀抽象
type ProcessAPI interface {
	// Query 按 PID 读取进程元信息；进程不存在时返回 ErrProcessNotFound。
	Query(pid uint32) (ProcInfo, error)
	// KillVerified 查杀前先复核 PID 对应的可执行路径与启动时间是否与 token 一致，
	// 不一致判定为 PID 复用，返回 ErrTokenMismatch 拒杀；force=true 跳过温和退出直接强杀。
	KillVerified(ctx context.Context, token VerifyToken, force bool) error
	// IsProtected 判断进程是否属于系统红线（csrss/winlogon 等关键进程），命中即禁止查杀。
	IsProtected(pid uint32, info ProcInfo) bool
}

// JobAPI Job Object 管理抽象
type JobAPI interface {
	// Create 创建 Job Object 并返回句柄封装；默认开启 KILL_ON_JOB_CLOSE。
	Create() (Job, error)
}

// KeepAwakeScope 防休眠层面位标志（可按位组合）。
type KeepAwakeScope uint8

const (
	KeepAwakeSystem  KeepAwakeScope = 1 << iota // 阻止系统休眠
	KeepAwakeDisplay                            // 阻止显示器自动熄灭
)

// KeepAwakeAPI 防休眠引用计数聚合器抽象（Windows 实现封 SetThreadExecutionState，
// 其余平台可给空操作降级）。
//
// 语义（合同）：
//   - 每个能力持有人（holder，如 "msgboard"）以唯一名字 Acquire 一份诉求，做完
//     Release；系统级休眠阻止状态是所有活跃持有人 scope 的并集；
//   - 多持有人交叠互不踩踏：任一持有人在场即保持阻止；最后一个 Release 后
//     （计数归零）才真正撤销系统状态；
//   - 同一持有人重复 Acquire 为幂等改判（后写覆盖前写的 scope，不加计数），
//     Release 亦幂等——持有人名字本身就是租约，宁可少计一次也不要泄露
//     "永远在防休眠"的常驻状态。
type KeepAwakeAPI interface {
	// Acquire 为 holder 申请阻止休眠（scope 不得为 0）。
	Acquire(holder string, scope KeepAwakeScope) error
	// Release 释放 holder 的诉求（holder 未在场时静默无操作）。
	Release(holder string) error
	// Holders 返回当前活跃持有人名单（有序，诊断与测试用）。
	Holders() []string
}

// Platform 统一聚合接口
type Platform interface {
	Network() NetworkAPI
	Port() PortAPI
	Process() ProcessAPI
	Job() JobAPI
	// AppPackage 管理当前用户注册的 Windows 应用包。
	AppPackage() apppackage.API
	// KeepAwake 防休眠引用计数聚合器（挂牌/录制等"我在场别睡"类能力的公共设施）。
	KeepAwake() KeepAwakeAPI
	// DesktopDir 返回当前用户桌面目录（供便携工具的桌面快捷方式落点）
	DesktopDir() (string, error)
	// CreateDesktopShortcut 在桌面创建快捷方式（同名覆盖）
	CreateDesktopShortcut(name, target, workDir string) error
	// OpenURL 以默认浏览器打开链接
	OpenURL(url string) error
}
