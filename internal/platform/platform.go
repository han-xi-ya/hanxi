package platform

import (
	"context"
	"errors"
	"time"

	"hubkit/internal/platform/apppackage"
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
	Assign(pid uint32) error
	Close() error
	Terminate(exitCode uint32) error
	// SetAllowKillOnClose 动态调整 KILL_ON_JOB_CLOSE 限制：
	// true（默认，创建即启用）= HubKit 退出/崩溃时内核连带杀 Job 内进程；
	// false = 工具独立运行，HubKit 退出完全不影响它（"不随 HubKit 关闭"开关）。
	SetAllowKillOnClose(enabled bool) error
}

// NetworkAPI 网络与接口抽象
type NetworkAPI interface {
	Adapters() ([]Adapter, error)
	DefaultAdapter() (*Adapter, error)
	NeighborTable() ([]Neighbor, error)
	Ping(ctx context.Context, ip string, timeout time.Duration) (rtt time.Duration, ok bool, err error)
}

// PortAPI 端口与连接表抽象
type PortAPI interface {
	TCPTable(family Family) ([]TCPRow, error)
	UDPTable(family Family) ([]UDPRow, error)
}

// ProcessAPI 进程管理与安全查杀抽象
type ProcessAPI interface {
	Query(pid uint32) (ProcInfo, error)
	KillVerified(ctx context.Context, token VerifyToken, force bool) error
	IsProtected(pid uint32, info ProcInfo) bool
}

// JobAPI Job Object 管理抽象
type JobAPI interface {
	Create() (Job, error)
}

// Platform 统一聚合接口
type Platform interface {
	Network() NetworkAPI
	Port() PortAPI
	Process() ProcessAPI
	Job() JobAPI
	// AppPackage 管理当前用户注册的 Windows 应用包。
	AppPackage() apppackage.API
	// DesktopDir 返回当前用户桌面目录（供便携工具的桌面快捷方式落点）
	DesktopDir() (string, error)
	// CreateDesktopShortcut 在桌面创建快捷方式（同名覆盖）
	CreateDesktopShortcut(name, target, workDir string) error
	// OpenURL 以默认浏览器打开链接
	OpenURL(url string) error
}
