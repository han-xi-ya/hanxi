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
	StateExternal State = "external" // 外部用户自启的 BCU 实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；BCU 既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

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
	Version string // 绑定版本（如 6.2.0）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // BCUninstaller.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("BCUninstaller.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine BCU 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 BCU 专属账目（退出码/停止时刻/740 提权指引覆盖）与
// EnumWindows 按 PID 的 WM_CLOSE 优雅退出钩子、信使唤窗。
type Engine struct {
	mu          sync.Mutex
	exitCode    int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt   time.Time // 自有实例最近一次落终态的时刻
	errOverride string    // spawn 失败时按 740 提权指引覆盖内核原始文案（Start 时清零）

	sup   *sup.Engine
	probe BCUProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe BCUProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内先向自有实例全部顶层窗口投递 WM_CLOSE
	// （BCU 的 FormClosing 走正常关闭路径：设置落盘、互斥体释放、退出码 0），
	// 超时由内核 JobObject 强杀兜底。BCU 无固定窗口类名，只能按 PID 枚举。
	e.sup.SetQuitHook(func(context.Context) error {
		s := e.sup.Snapshot()
		if s.PID == 0 {
			return errors.New("实例没有可投递消息的进程")
		}
		postCloseByPID(s.PID)
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——便携设置按相对路径解析依赖此）。
// BCU 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类兜底
// （ReadyTimeout=0 即 markeron 同款冷启动语义）。
//
// 双层锚点契约（勿退，见 prober.go 包注释 innerExeRel 段）：opts.Exe 必须来自
// version.ResolveExe 解析出的 win-x64 真身。若把外层 bootstrapper 交给内核，
// 外层约 20ms 接力自退 + 内层存活持锁将被内核 wait 的 external-takeover 分类
// （stopping → 探针仍见目标存活 → external）误判，真身又生于绑 Job 之前不受
// KILL_ON_JOB_CLOSE 管辖——托管能力全失。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.errOverride = ""
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：BCU 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（零 fork 承诺）。
	err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
	if err != nil {
		// requireAdministrator 清单 + 未提权父进程：内核落 failed 的原始文案是
		// 直译 740，对用户不友好——覆盖为提权指引后重发终态快照（事件契约
		// 保持"failed 即带管理员指引"的既有口径），错误本身仍如实上抛。
		if hint := elevateHint(err); hint != "" {
			e.mu.Lock()
			e.errOverride = hint
			e.mu.Unlock()
			e.emit(e.Snapshot())
		}
		return err
	}
	return nil
}

// OpenWindow 完成单实例协议的窗口唤起：拉起同路径第二个 BCU 实例充当
// "信使"，第二实例查找主进程后 SetForegroundWindow 唤起主窗口（见
// messenger.go 的拉起纪律）；主实例是否存在由 service 层依据快照判定。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	if err := e.spawnMessenger(exe); err != nil {
		// 外部实例通常由用户以管理员身份自启（BCU 卸载操作本身要提权），
		// 未提权的 Hanxi 拉起信使同样被 740 直拒——指引与 Start 路径一致。
		if hint := elevateHint(err); hint != "" {
			return false, errors.New(hint)
		}
		return false, err
	}
	e.emit(e.Snapshot())
	return true, nil
}

// Quit 退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 按 PID 投递
// WM_CLOSE → 宽限 closeGracePeriod 内等进程自然收口（FormClosing 正常关闭
// 路径：设置落盘、互斥体释放、退出码 0）→ 超时（对话框挂起/主窗口未创建
// 等）JobObject 强杀兜底。幂等；external 态按既有契约映射为无操作成功。
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

// RefreshExternal 探测命名互斥体校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
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

// WaitReady 阻塞等待 BCU 实例就绪（单实例互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 自有实例的可见顶层窗口是否存在——空闲自动退出的豁免信号
// （探针按 PID 枚举，属 BCU 领域能力，不经内核）。
func (e *Engine) IsMainWindowOpen() bool {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.PID == 0 {
		return false
	}
	return e.probe.IsMainWindowOpen(s.PID)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → bcu 形状映射 ----------

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
		Error:     e.mapErrorMessage(s),
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
//	supervisor stopping → running（bcu 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 bcu 既有失败文案（前置条件：已持 e.mu）：
//   - spawn 提权失败覆盖为 740 管理员指引（errOverride）；
//   - 内核异常退出消息改回"BCU 异常退出（退出码 N）。请尝试重新安装该版本"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func (e *Engine) mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if e.errOverride != "" {
			return e.errOverride
		}
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("BCU 异常退出（退出码 %d）。请尝试重新安装该版本", code)
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

// supProbe 把 BCUProbe（命名互斥体存在性）适配为内核统一探针契约。
// 互斥体探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p BCUProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
