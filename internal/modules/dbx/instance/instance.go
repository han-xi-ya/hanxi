package instance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"hanxi/internal/platform"
	sup "hanxi/packages/go/supervisor"
)

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（极短窗口）
	StateRunning  State = "running"  // 自有 Job 托管主实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启（或应用自带备份计划任务拉起）的 DBX 主实例
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// DBX 上游关窗语义为 CloseRequested → 驻托盘或弹询问确认框（无 CLI quit）——
// 驻托盘/模态框在场时 WM_CLOSE 只藏窗或排队，进程不会在宽限内自然收口，
// 强杀兜底正是为其设计（SQLite WAL 数据由进程级 crash-safe 保障）。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s（家族同值）。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；dbx 既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

// EnvDataDirKey 托管启动注入的数据改道环境变量名（阶段 0 裁决：上游实证 env
// 优先级最高，压过 portable.dbx 标记与注册表装机态）；路径与 activeVersion
// 无关、跨版本共享，收口在 Hanxi 数据根。导出供 service 层文档/测试引用。
const EnvDataDirKey = "DBX_DATA_DIR"

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"` // state==external 时为 true
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本 vX.Y.Z
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // DBX.exe 绝对路径（版本隔离目录内）
	// DataDir 受控数据目录（Hanxi 数据根下稳定路径）。非空时经
	// DBX_DATA_DIR 注入子进程环境——数据不随版本目录走、删版本不丢数据；
	// 空 = 不注入（仅测试路径；生产 service 层恒注入）。
	DataDir string
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("DBX.exe 路径不能为空")
	}
	return nil
}

// dataDirEnv 组装受控环境变量注入（内核 Spec.Env 形状："KEY=VALUE"）；
// dataDir 为空返回 nil（纯继承父进程环境）。抽为纯函数供单测钉死注入面。
func dataDirEnv(dataDir string) []string {
	if dataDir == "" {
		return nil
	}
	return []string{EnvDataDirKey + "=" + dataDir}
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine DBX 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 DBX 专属账目（退出码/停止时刻）与信使唤窗、WM_CLOSE 优雅退出钩子。
// 唤窗走 tauri-plugin-single-instance handoff（信使，ccswitch 同族），
// 不做按 PID 直操作唤回：上游第二实例在场时必然让位，信使即唯一正解。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe DBXProbe
	cb    Callbacks

	// specArgs 真机冒烟专用注入缝：生产 DBX 恒无参拉起（nil，无参即开
	// 主窗口），测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能稳定存活
	// 的确定性命令参数。
	specArgs []string

	// spawnMessenger 信使拉起接缝（默认真实 spawn，测试注入）。签名携带
	// dataDir：接管场（无主实例时信使原地转正）同样吃到 DBX_DATA_DIR 注入，
	// 托管生命周期内数据改道无洞。
	spawnMessenger func(exe, dataDir string) error
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe DBXProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向自有实例 PID 的全部顶层窗口投递
	// WM_CLOSE（tauri CloseRequested 按用户设置 exit(0) 驻托盘或弹询问），
	// 超时由内核 JobObject 强杀兜底。
	//
	// 目标选择偏离 ccswitch 模板一处、如实注明：ccswitch 经 FindWindow
	// （类 -sic / 名 -siw）向单实例隐藏消息窗投 WM_CLOSE（真机验证过），
	// DBX 未做该实证——关窗语义（驻托盘/弹询问）挂在主窗 CloseRequested 上
	// 才有用户可感知行为，故按 gonavi 形制向自有 PID 的顶层窗投递（可见主窗
	// 必然在场；启动初期无窗时静默 no-op，收口交给宽限+强杀兜底）。
	e.sup.SetQuitHook(func(context.Context) error {
		if pid := e.sup.Snapshot().PID; pid != 0 {
			postCloseByPID(pid)
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——portable.dbx 按相对目录识别
// 依赖此）。DBX 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出
// 分类兜底（ReadyTimeout=0 即 markeron 同款冷启动语义）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：DBX 是 GUI 子系统程序（Tauri v2），
	// 既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
		Env:           dataDirEnv(opts.DataDir),
	})
}

// OpenWindow 完成单实例协议的窗口唤起：拉起同路径第二个 DBX 实例充当
// "信使"，tauri-plugin-single-instance 回调无条件 show+focus 主窗口（见
// messenger.go 的拉起纪律）；主实例是否存在由 service 层依据快照判定。
func (e *Engine) OpenWindow(exe, dataDir string) (opened bool, err error) {
	if err := e.spawnMessenger(exe, dataDir); err != nil {
		return false, err
	}
	e.emit(e.Snapshot())
	return true, nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 WM_CLOSE
// → 宽限 closeGracePeriod 内等进程自然收口（关窗即退场）→ 超时 JobObject 强杀
// 兜底（驻托盘/弹询问模态框场；DBX 数据为 SQLite WAL，进程级终止由 SQLite
// crash-safe 保障）。幂等；external 态按既有契约映射为无操作成功。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待动画）。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 状态不在管辖范围内：指引文案由 service 层给出
		}
		return err
	}
	return nil
}

// RefreshExternal 探测校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
// 判据含进程名兜底，故 Quit 后托管备份 worker（--managed-backup-worker，
// 脱出 Job 的异主进程）残留会被如实甄别为 external。
func (e *Engine) RefreshExternal() {
	e.sup.RefreshExternal()
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	outer := e.sup.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(outer)
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	return s.Exe
}

// WaitReady 阻塞等待 DBX 主实例就绪（互斥体出现；拒探场为进程/主窗判据），
// 超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 主窗口是否可见——探针领域能力（不经内核；空闲自动退出
// 等豁免判据的备位，本模块当前无消费方，保留契约完整性）。
func (e *Engine) IsMainWindowOpen() bool {
	return e.probe.IsMainWindowOpen()
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → dbx 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
func (e *Engine) onSupState(s sup.Snapshot) {
	var snap Snapshot
	e.mu.Lock()
	snap = e.snapshotLocked(s)
	e.mu.Unlock()
	e.emit(snap)
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked(s sup.Snapshot) Snapshot {
	switch s.State {
	case sup.StateStopped, sup.StateFailed:
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	}
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			e.exitCode = code
		}
	}
	return Snapshot{
		Version:   s.Version,
		State:     mapState(s.State),
		PID:       s.PID,
		ExitCode:  e.exitCode,
		Error:     mapErrorMessage(s),
		External:  s.State == sup.StateExternal,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（dbx 既有词表无 stopping：终止窗口对前端保持
//	                      运行语义，收口后由 stopped/failed 终态广播纠正）
//	supervisor external → external
//	supervisor failed   → failed
func mapState(s sup.State) State {
	switch s {
	case sup.StateStarting:
		return StateStarting
	case sup.StateRunning, sup.StateStopping:
		return StateRunning
	case sup.StateExternal:
		return StateExternal
	case sup.StateFailed:
		return StateFailed
	default:
		return StateStopped
	}
}

// kernelAbnormalExitRe 匹配内核异常退出文案中的退出码。措辞耦合自
// supervisor.wait 的分类消息"托管进程异常退出（退出码 %d）"——内核文案变更时
// 本处回退为透传 Error（ExitCode 保持账目值），不会崩溃，仅少一层改写。
var kernelAbnormalExitRe = regexp.MustCompile(`托管进程异常退出（退出码 (-?\d+)）`)

// exitCodeFromKernelMessage 从内核异常退出文案中提取退出码。
func exitCodeFromKernelMessage(msg string) (int, bool) {
	g := kernelAbnormalExitRe.FindStringSubmatch(msg)
	if g == nil {
		return 0, false
	}
	var code int
	if _, err := fmt.Sscanf(g[1], "%d", &code); err != nil {
		return 0, false
	}
	return code, true
}

// mapErrorMessage 还原 dbx 既有失败文案：
//   - 内核异常退出消息改回"DBX 异常退出（退出码 N）。请确认已安装
//     WebView2 Runtime"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("DBX 异常退出（退出码 %d）。请确认已安装 WebView2 Runtime（Win11 系统自带）", code)
		}
	}
	if s.State == sup.StateStopped && s.Error == manualStopWording {
		return ""
	}
	return s.Error
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 DBXProbe（互斥体主判据 + 拒探进程/主窗兜底）适配为内核统一
// 探针契约。探测均为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，
// 永不报错）。进程枚举可得时附带首个命中 PID 的 ProcInfo——external 入账
// 据此充实快照 PID（ gonavi 同法）；纯互斥体命中（elevated 他主进程枚举不
// 可得的极端场）只报存活，PID 留零，沿用 ccswitch 口径。
type supProbe struct{ p DBXProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	if !s.p.IsRunning() {
		return false, nil, nil
	}
	if pids := s.p.FindPIDs(); len(pids) > 0 {
		return true, &platform.ProcInfo{PID: pids[0], Name: exeImageName}, nil
	}
	return true, nil, nil
}
