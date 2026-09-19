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
	StateExternal State = "external" // 外部用户自启的 PaperTodo 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的"exit 命令信使"优雅退出宽限：主实例收到命令后自行
// 保存数据并退出；超时（管道未建好/主实例卡死）则 JobObject 强杀兜底。
// 包级变量仅为单测可压缩等待时长，生产值保持 3s（便签含图片库落盘稍重）。
var closeGracePeriod = 3 * time.Second

// manualStopWording 内核手动停止的收口文案；papertodo 既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原（ccswitch/rufus 同款折回）。
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

// StartOptions 启动参数：由 service 层解析托管安装后填充。
type StartOptions struct {
	Version string // 绑定版本（meta tag，如 v3.31）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响便签）。
	Detached bool
	Exe      string // PaperTodo.exe 绝对路径（托管目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("PaperTodo.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine PaperTodo 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 PaperTodo 专属账目（退出码/停止时刻）与命令信使（show/hide 唤窗
// 收拢、exit 优雅退出钩子）。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe PaperProbe
	cb    Callbacks

	// launchMessenger 命令信使拉起接缝（默认真实 spawn，测试注入）：
	// PaperTodo 信使带命令行参数（上游把它经命名管道转发给主实例执行）。
	launchMessenger func(exe string, args ...string) error
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe PaperProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:           probe,
		cb:              cb,
		launchMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道（官方命令词表实证）：Quit 的 grace 窗口内先拉起携 exit
	// 参数的信使——主实例经命名管道收到后保存数据自退，比 WM_CLOSE/裸强杀
	// 更干净且天然覆盖"优雅退出保存数据"；信使投递失败如实吞掉（管道监听
	// 未就绪等场），超时由内核 JobObject 强杀兜底（上游"写盘前自动快照备份"
	// 保证强杀最坏也只回滚到最近快照）。
	e.sup.SetQuitHook(func(context.Context) error {
		s := e.sup.Snapshot()
		if s.Exe != "" {
			_ = e.launchMessenger(s.Exe, "exit")
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——便签数据按相对路径解析依赖此）。
// PaperTodo 无后台启动 CLI，启动语义即"无参拉起 → 纸片出现在桌面 + 托盘常驻"。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类
// 兜底（ReadyTimeout=0 即 markeron 同款冷启动语义——我方进程信使化自退后，
// 互斥体仍被外部主实例持有 → external）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：PaperTodo 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// OpenWindow 唤回纸片：向主实例（自有或外部）发送 show 命令信使——
// 上游回调 ShowAllPapers()，把散落/折叠的纸片全部找回（见 messenger.go 的拉起纪律）。
func (e *Engine) OpenWindow(exe string) error {
	if err := e.launchMessenger(exe, "show"); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	e.emit(e.Snapshot())
	return nil
}

// HidePapers 收拢纸片：hide 命令信使（主实例在场才有意义，service 层限定状态）。
func (e *Engine) HidePapers(exe string) error {
	if err := e.launchMessenger(exe, "hide"); err != nil {
		return fmt.Errorf("拉起收拢信使失败: %w", err)
	}
	return nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 派 exit 命令
// 信使 → 宽限 closeGracePeriod 内等进程自然收口（保存数据后自退）→ 超时
// JobObject 强杀兜底。幂等；external 态按既有契约映射为无操作成功（指引文案
// 由 service 层给出）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不派 exit 信使、不等宽限，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待保存动画）。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 状态不在管辖范围内
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

// WaitReady 阻塞等待 PaperTodo 实例就绪（单实例互斥体出现），超时返回 false。
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

// ---------- 内核 → papertodo 形状映射 ----------

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
//	supervisor stopping → running（papertodo 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 papertodo 既有失败文案：内核异常退出消息改回
// "PaperTodo 异常退出（退出码 N）。若使用 no-runtime 精简变体，请确认…"
// （运行时依赖指引是本模块领域文案）；内核手动停止的"已手动停止"折回本引擎
// 既有的空文案（stopped 态不带话术）；其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("PaperTodo 异常退出（退出码 %d）。若使用 no-runtime 精简变体，请确认系统已安装 .NET 10 桌面运行时（环境检测页可查看）", code)
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

// supProbe 把 PaperProbe（命名互斥体存在性）适配为内核统一探针契约。
// 互斥体探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p PaperProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
