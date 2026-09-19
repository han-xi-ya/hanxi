// Package supervisor 是共享托管内核：把全仓各模块 instance/*.go 蓝本中反复实现的
// "进程治理主流程"收口为一份可注入、可单测的引擎实现。
//
// # 蓝本共性（本包沉淀的纪律）
//   - 启动三段式：spawn → Job Object Create/Assign（可选 SetAllowKillOnClose(false)
//     解除退出联动，即"不随 Hanxi 关闭"开关）→ 就绪探测/分类入账；
//   - 并发纪律：状态锁 + Start/Stop 操作锁分离，cmd.Wait 只有一个所有者（监督协程），
//     上一代进程未收口前拒绝新启动（收口 = Wait 返回且终态已回写）；
//   - 退出分类：手动停止 → stopped；自有进程退出但探针仍见目标存活（冷启动竞速，
//     我方是转发让位的第二实例）→ external；退出码 0 → stopped；非零 → failed；
//   - 外部实例只甄别、只指引，绝不由本引擎强杀（Stop 返回 ErrExternal）；
//   - 静止态用探针校正 external/stopped（轮询/间隙补偿），运行/启动/退出中不探测
//     （探测到的正是自己，会误导状态机）；
//   - PID 复用防护：注入 platform.ProcessAPI 后，启动时建立 VerifyToken 身份，
//     兜底强杀路径先经 KillVerified 复核（Job Terminate 走句柄语义不受复用影响）。
//
// # 与蓝本的刻意差异
//   - Probe 收敛为单一 Inspect 事实源（外部性 + ProcInfo），就绪等待与外部判定共用，
//     取代各蓝本 IsRunning/PortOpen/WaitForReady 的碎片接口；
//   - 优雅退出通道（管道命令/WM_CLOSE/HTTP shutdown）经 SetQuitHook 注入，
//     统一为 Stop 的 grace 窗口语义（蓝本散落在 Quit/ResetState/reload 等处）；
//   - 进程树监督（rustdesk/subnetdesk 的 packer 外层早退形态）、日志环形缓冲与
//     脱敏、连接状态嗅探等模块策略明确不进本包，留在各模块适配层；本包仅按
//     Callbacks.OnLog 逐行透传子进程 stdout/stderr（不缓冲、不脱敏、不限速）。
//
// 平台差异收口：Spec.HideWindow 经包内 child_windows.go / child_other.go 两个
// 最小平台文件消化（Windows 注入 CREATE_NO_WINDOW，其他平台 no-op），主流程文件
// 零平台特异代码，GOOS=linux 可编译。
//
// 本包零框架依赖（仅 hanxi/internal/platform 抽象），便于单元测试。
package supervisor

import (
	"context"
	"errors"
	"time"

	"hanxi/internal/platform"
)

// State 引擎状态机，与既有各模块 instance.Snapshot 的语义对齐：
// stopped → starting → running → (stopped | failed | external)，
// Stop 过程中经 stopping 中间态收敛到终态。
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建与就绪等待窗口
	StateRunning  State = "running"  // 自有 Job 托管实例运行中
	StateExternal State = "external" // 外部自行启动的目标实例（非本引擎托管）
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateStopping State = "stopping" // 已发出终止请求、等待进程收尾
)

// Ownership 实例归属判定：外部（非本 supervisor 启动）/ 受管（自家 pid）/ 无。
type Ownership int

const (
	// OwnNone 目标未在运行，或探针失败无法确认（保守按"无"处理，不触碰任何进程）。
	OwnNone Ownership = iota
	// OwnExternal 目标在运行，但不是本引擎启动的实例。
	OwnExternal
	// OwnManaged 目标在运行且为本引擎自有进程（自家 pid，句柄在手）。
	OwnManaged
)

// String 实现 fmt.Stringer，便于诊断输出。
func (o Ownership) String() string {
	switch o {
	case OwnExternal:
		return "external"
	case OwnManaged:
		return "managed"
	default:
		return "none"
	}
}

// Probe 实例存在性探针（互斥体/端口/窗口/管道/命令行枚举各族实现由模块注入）。
type Probe interface {
	// Inspect 报告外部事实：目标工具是否已在运行、及其 ProcInfo（可 nil）。
	// 探针实现自身应带超时兜底；引擎侧另设 inspectTimeout 上限。
	Inspect(ctx context.Context) (running bool, info *platform.ProcInfo, err error)
}

// Snapshot 状态快照（事件形状由模块映射为各自的 wails 事件载荷）。
type Snapshot struct {
	State   State     `json:"state"`
	PID     uint32    `json:"pid"`     // 受管态=自有进程 pid；external=探针报告值（未知为 0）
	Version string    `json:"version"` // 最近一次 Start 绑定的版本
	Exe     string    `json:"exe"`     // 最近一次托管的可执行路径（external 态清空）
	Error   string    `json:"error"`   // failed 态的用户可读原因
	Managed bool      `json:"managed"` // 当前状态是否指向自有进程（starting/running/stopping）
	Since   time.Time `json:"since"`   // 最近一次已知启动时刻
}

// Spec 启动规格（受控字段，无任意 shell：Exe/Args 原样传给 CreateProcess）。
type Spec struct {
	// Version 绑定版本标签（仅入 Snapshot，供模块事件映射；不参与进程治理）。
	Version       string
	Exe           string        // 可执行文件绝对路径，必填
	Args          []string      // 参数列表（不经 shell 解析）
	WorkingDir    string        // 空 = 锁定到 Exe 所在目录（蓝本惯例）
	DetachFromJob bool          // true = SetAllowKillOnClose(false)，"不随宿主关"开关
	ReadyTimeout  time.Duration // 0 = 不等待就绪（仅 spawn + 绑定入账）
	// HideWindow true = spawn 时子进程不弹控制台窗口（Windows 经 SysProcAttr 注入
	// CREATE_NO_WINDOW + HideWindow，其他平台 no-op；引擎侧只透传，平台文件消化差异）。
	HideWindow bool
	// Env 追加到子进程继承环境之后的受控注入（"KEY=VALUE" 形式，如 DDNS_GO_DAEMON=1；
	// 空 = 纯继承当前进程环境，cmd.Env 保持 nil 语义）。
	Env []string
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(Snapshot)
	// OnLog 逐行转发受管进程 stdout/stderr（两路泵协程，行内容不含换行符，两路
	// 之间不保证相对顺序）。本包只搬运不加工：敏感信息脱敏、缓冲、限速等策略由
	// 模块层在回调内自理；未注册时引擎不获取输出管道（零开销）。
	OnLog func(line string)
}

// 引擎语义错误（errors.Is 判定）。
var (
	// ErrExternal 检测到外部实例正在运行：不由 supervisor 强杀，调用方给出指引。
	ErrExternal = errors.New("supervisor: instance is external, not managed by this engine")
	// ErrBusy 上一个受管进程尚未收口（wait 未完成），拒绝新启动。
	ErrBusy = errors.New("supervisor: previous managed process not settled yet")
)
