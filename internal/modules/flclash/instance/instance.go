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
	StateExternal State = "external" // 外部用户自启的 FlClash 实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"驻留托盘"设置场（WM_CLOSE 只藏窗不退进程）。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；flclash 既有快照口径在 stopped
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
	Version string // 绑定版本（如 0.8.96）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool   // 绑定版本（如 0.8.96）
	Exe      string // FlClash.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("FlClash.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine FlClash 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 FlClash 专属账目（退出码/停止时刻）与唤窗直操作、WM_CLOSE 优雅退出钩子。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe FlClashProbe
	cb    Callbacks

	// specArgs 真机冒烟专用注入缝：生产 FlClash 恒无参拉起（nil，无参即开
	// 主窗口并驻托盘），测试注入让替身进程（cmd.exe）在精简 stdin 环境下也
	// 能稳定存活的确定性命令参数。
	specArgs []string

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe FlClashProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向自有实例 PID 的全部顶层窗口投递
	// WM_CLOSE（Flutter 默认关窗即退 exit(0)，驻托盘设置场则仅藏窗），
	// 超时由内核 JobObject 强杀兜底。窗口类名不可预测，按 PID 枚举（模块领域知识）。
	e.sup.SetQuitHook(func(context.Context) error {
		if pid := e.sup.Snapshot().PID; pid != 0 {
			postCloseByPID(pid)
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——Flutter 资源按相对路径解析依赖此）。
// FlClash 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示（托盘驻留）"。
// 本方法不做进程存在性探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出
// 分类兜底（ReadyTimeout=0 即 markeron/ccswitch 同款冷启动语义）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：FlClash 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// RestoreWindow 唤起自有实例窗口——Win32 直操作（EnumWindows 按 PID 定位顶层
// 窗口 → SW_RESTORE + SetForegroundWindow，见 close_windows.go）。
// FlClash 的第二实例有退出无唤窗（文件锁竞速直接 exit(0)），"打开窗口"不能
// 走信使只能直操作——信使语义与 markeron 的差异论证见 messenger.go。
func (e *Engine) RestoreWindow() {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.PID == 0 {
		return
	}
	restoreWindowByPID(s.PID)
	e.emit(e.Snapshot())
}

// RestoreExternalWindow 唤起外部实例窗口（外部 PID 由进程枚举探测提供）。
// 与自有实例同走 Win32 直操作路径（上游契约对"谁拉起的不区分"）。
func (e *Engine) RestoreExternalWindow(pids []uint32) {
	for _, pid := range pids {
		restoreWindowByPID(pid)
	}
}

// triggerSingleInstanceCheck 信使路径（保留能力，生产唤窗不走）：二次拉起仅触发
// 上游文件锁单实例检查后自退，不产生任何窗口动作（messenger.go 论证）。
// 保留为接缝：若上游未来在单实例回调补上 show+focus，唤窗可一行切换回信使语义。
func (e *Engine) triggerSingleInstanceCheck(exe string) error {
	return e.spawnMessenger(exe)
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 按 PID 投递
// WM_CLOSE → 宽限 closeGracePeriod 内等进程自然收口（关窗即退场）→ 超时
// JobObject 强杀兜底（驻留托盘场）。幂等；external 态按既有契约映射为无操作成功。
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

// RefreshExternal 进程快照探测校正 external/stopped 状态（委托内核）。
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

// WaitReady 阻塞等待 FlClash 实例就绪（进程出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 自有实例的可见顶层窗口是否存在——空闲自动退出的豁免信号
// （探针领域能力，不经内核）。
func (e *Engine) IsMainWindowOpen() bool {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.PID == 0 {
		return false
	}
	return e.probe.IsMainWindowOpen([]uint32{s.PID})
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ExternalPIDs 外部实例 PID 列表（唤窗用；仅 external 语义下由 service 调用）。
func (e *Engine) ExternalPIDs() []uint32 {
	return e.probe.FindPIDs()
}

// ---------- 内核 → flclash 形状映射 ----------

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
//	supervisor stopping → running（flclash 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 flclash 既有失败文案：
//   - 内核异常退出消息改回"FlClash 异常退出（退出码 N）。请尝试重新安装该版本"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("FlClash 异常退出（退出码 %d）。请尝试重新安装该版本", code)
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

// supProbe 把 FlClashProbe（进程快照枚举）适配为内核统一探针契约。
// FlClash 无命名互斥体（单实例是文件锁），进程名 FlClash.exe 是唯一稳定标识；
// 与 markeron/ccswitch 的互斥体探针不同，本探针能给出存活 PID——Inspect 附带
// 首个命中进程的 ProcInfo，内核 external 入账时据此充实快照 PID。
// 快照枚举为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错）。
type supProbe struct{ p FlClashProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	pids := s.p.FindPIDs()
	if len(pids) == 0 {
		return false, nil, nil
	}
	return true, &platform.ProcInfo{PID: pids[0], Name: exeName}, nil
}
