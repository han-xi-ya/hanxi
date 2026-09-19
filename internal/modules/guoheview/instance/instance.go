package instance

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
	StateRunning  State = "running"  // 自有 Job 托管实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 存在用户自行启动的看图实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 真机实测关窗即退在 3s 内完成，2s 宽限 + 强杀兜底足够收敛。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；guoheview 既有快照口径在 stopped
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
	Version string // 绑定版本 vX.Y.Z.W
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // GuoheView.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("GuoheView.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine 果核看图托管实例引擎：组合内核 supervisor.Engine（进程创建、Job
// 绑定、退出分类、强杀兜底收口内核），本层持有多实例上游的领域能力与账目：
// 自有单托管实例模型（引擎只追踪自己拉起的那一个进程，用户自行打开的窗口
// 归 external 感知）、退出码/停止时刻、按自有 PID 收敛的唤窗与 WM_CLOSE
// 钩子、独立窗口信使拉起。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe ViewProbe
	cb    Callbacks

	// specArgs 真机冒烟专用注入缝：生产果核看图恒无参拉起（nil，无参即开
	// 浏览主窗口），测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能
	// 稳定存活的确定性命令参数。
	specArgs []string

	// launchDetached 独立窗口拉起接缝（默认真实 spawn，测试注入）。
	launchDetached func(exe string) error
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe ViewProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		launchDetached: launchDetachedWindow,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{e}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向自有 PID 的可见窗口投递 WM_CLOSE
	// （果核看图关窗即退，真机实测），超时由内核 JobObject 强杀兜底。
	// 刻意按自有 PID 投递而非按进程名全量广播：上游是多实例应用，用户双击
	// 图片自行打开的看图窗口绝不代关（误伤用户窗口是事故，见包注释契约）。
	e.sup.SetQuitHook(func(context.Context) error {
		postCloseFn(e.ownPID())
		return nil
	})
	return e
}

// Start 启动自有托管实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——portable.ini 便携语义按目录
// 解析依赖此）。果核看图是多实例应用，拉起即开窗，不存在"信使转发"路径。
// 本方法不做进程探测：外部实例并存是正常态，冷启动竞速的退出分类由内核
// wait() 兜底（ReadyTimeout=0 即 markeron 同款冷启动语义；就绪判定留在
// service 层经 WaitReady 完成）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// GUI 子系统程序，无需隐藏控制台窗口，也不做任何窗口干预（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// Focus 把自有托管实例的主窗口恢复到前台（多实例模型的"唤窗"：只碰自己的
// 窗口——按内核登记的自有 PID 过滤，用户自行打开的看图窗口不被打扰）。
// 窗口尚未出现（非 running/启动初期）返回 false，由 service 层降级提示。
func (e *Engine) Focus() bool {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning {
		return false
	}
	return e.probe.FocusMainWindow(s.PID)
}

// FocusExternal 唤回任一外部 GuoheView 窗口（用户自行打开的看图实例）。
// 只做前台切换，不接管、不绑 Job——外部实例归属用户，永不归托管生命周期。
func (e *Engine) FocusExternal() bool {
	return e.probe.FocusAnyWindow()
}

// LaunchDetachedWindow 独立拉起一个新看图窗口：spawn 后立即 Release，
// 刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期——关窗/退出
// 都由用户自行决定，Hanxi 关闭也不牵连）。不经内核（supervisor 只管自有
// 托管实例的进程治理）。此决策请勿在后续维护中"好心"改成 Wait/绑 Job
// （家族信使模式同源教训，此处是真实窗口而非信使进程）。
func (e *Engine) LaunchDetachedWindow(exe string) error {
	return e.launchDetached(exe)
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 按自有 PID
// 投递 WM_CLOSE → 宽限 closeGracePeriod 内等进程自然收口（关窗即退，config.ini
// 由程序退出时落盘，设置零丢失）→ 超时 JobObject 强杀兜底（文件确认框挂起等
// 罕见场；强杀仅损失未落盘的窗口位置类偏好，图片浏览本身无状态）。
// 幂等；external 态按既有契约映射为无操作成功（指引文案由 service 层给出）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止（应用退出 Shutdown 通道用，无需等待动画）。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE
// 内核兜底。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 状态不在管辖范围内（用户自行打开的窗口归用户）
		}
		return err
	}
	return nil
}

// RefreshExternal 探测外部实例校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的可能是自己的托管进程。
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

// WaitReady 阻塞等待自有实例就绪（可见窗口出现），超时返回 false。
// 领域探测能力，不经内核（内核就绪语义按 Inspect 进程存在性，与"窗口出现"
// 判据不同，保持既有口径零漂移）。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ownPID 返回当前受管进程的 PID：仅内核确认自有进程在世（Managed 态）时有效，
// 其余返回 0——WM_CLOSE 钩子据此收敛投递目标，绝不按进程名广播误伤外部实例。
func (e *Engine) ownPID() uint32 {
	s := e.sup.Snapshot()
	if s.Managed {
		return s.PID
	}
	return 0
}

// ---------- 内核 → guoheview 形状映射 ----------

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
//	supervisor stopping → running（guoheview 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 guoheview 既有失败文案：
//   - 内核异常退出消息改回"果核看图异常退出（退出码 N）。若反复出现，请在
//     版本管理重新安装"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("果核看图异常退出（退出码 %d）。若反复出现，请在版本管理重新安装", code)
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

// launchDetachedWindow 独立窗口真实拉起实现（LaunchDetachedWindow 的默认接缝）：
// GUI 子系统程序无参拉起即开浏览窗口，Start 后立即 Release 脱离本进程监管。
func launchDetachedWindow(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe) // 工作目录锁定：portable.ini 便携语义按目录解析
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起看图窗口失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// supProbe 把 ViewProbe（进程名快照探测）适配为内核统一探针契约。
// 多实例上游的公理冲突与处置（ADR-0002 §5 家族的边界确认）：内核 Inspect 的
// 单 ProcInfo 公理表达不了"同时存在 N 个 GuoheView 进程"的实例计数，且核外
// 无计数消费方（现状即 bool 判定）——计数账目留模块自持，本适配只回答
// "是否存在非自有的 GuoheView 进程"：归属判定按探针 PID（RunningBesides
// 排除自有托管 PID，消除自有进程退出瞬间 Toolhelp 快照仍列出自己导致的
// "外部接管"误判），ProcInfo 返回 nil 维持 external 态快照 PID=0 的原口径。
type supProbe struct{ e *Engine }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.e.probe.RunningBesides(s.e.ownPID()), nil, nil
}
