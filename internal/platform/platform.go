package platform

import (
	"context"
	"errors"
	"time"
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

// Adapter 网卡信息
type Adapter struct {
	Index        uint32   `json:"index"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	MAC          string   `json:"mac"`
	IPv4         []string `json:"ipv4"`
	IPv6         []string `json:"ipv6"`
	Gateway      string   `json:"gateway"`
	IsPhysical   bool     `json:"isPhysical"`
	IsLoopback   bool     `json:"isLoopback"`
	IsUp         bool     `json:"isUp"`
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
}
