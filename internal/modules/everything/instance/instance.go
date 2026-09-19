// Package instance 实现 Everything 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 Everything 领域适配：
//   - 窗口类双通道探针（TASKBAR_NOTIFICATION 托盘通知窗口类 / 命名互斥体兜底，
//     sup.Probe 形状适配见文件尾 supProbe；探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的运行模式（Mode，仅信息提示）、
//     退出码/停止时刻拼回既有事件契约（前端与 wails 事件载荷零漂移）；
//   - 三操作信使（见 messenger.go）：不属进程治理，不经内核 Engine——
//     唤起窗口=无参二次拉起走单实例协议；优雅退出=-quit 经 IPC 转发给主实例
//     （先落盘索引库再退出），注册为内核 Stop 的 QuitHook，宽限超时强杀兜底；
//   - 启动前配置播种（PreStart ini，隐藏托盘图标）属服务层编排，在 service
//     包落地，不在本包（进程治理与工具配置解耦）。
//
// Everything 无"标注开关"类二次语义，与 markeron 引擎的另一点本质差异：
// 运行模式（后台驻留/窗口）由启动参数决定，仅作信息提示，不承诺精确——
// 用户随时可以关闭搜索窗口而不退出实例，模式即回到后台。
//
// Everything 由内核启动后绑定 Windows Job Object：默认（Detached=false）启用
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE，Hanxi 无论以何种方式退出（托盘退出/崩溃/
// 强杀），内核都会连带终止 Everything 进程树，杜绝孤儿驻留；Detached=true
// （"不随 Hanxi 关闭"开关）则解除退出联动，工具独立常驻。
//
// 本包零框架依赖，便于单元测试。
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
	StateRunning  State = "running"  // 自有 Job 托管实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 Everything 实例（非本引擎托管）
)

// Mode 运行模式（信息提示用）
const (
	ModeBackground = "background" // -startup：后台驻留 + 托盘，不显示搜索窗
	ModeWindow     = "window"     // 无参启动：直接显示搜索窗
)

// quitGracePeriod Quit 的 -quit 优雅退出宽限：超时则 JobObject 强杀兜底
// （索引库状态由 Everything 定期落盘保障）。覆盖驻留后台的纯托盘形态。
// 包级变量仅为单测可压缩等待时长，生产值保持 5s。
var quitGracePeriod = 5 * time.Second

// manualStopWording 内核手动停止的收口文案；Everything 既有快照口径在 stopped
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
	Mode      string    `json:"mode"`     // 最近一次已知运行模式（信息提示，不承诺精确）
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本（如 1.5.0.1422b）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // Everything.exe 绝对路径（版本隔离目录内）
	Mode     string // ModeBackground / ModeWindow
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Everything.exe 路径不能为空")
	}
	if o.Mode != ModeBackground && o.Mode != ModeWindow {
		return fmt.Errorf("非法运行模式: %q", o.Mode)
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Everything 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 Everything 专属账目（运行模式推算、退出码/停止时刻）与信使通道。
type Engine struct {
	mu        sync.Mutex
	mode      string    // 最近一次已知运行模式（Start 落定；唤窗翻新；探测外部时清空）
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻
	prevSup   sup.State // 上一次内核广播态（external 入账时区分竞速接管/静止探测）

	sup   *sup.Engine
	probe EverythingProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 窗口信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe EverythingProbe, cb Callbacks) *Engine {
	e := &Engine{
		prevSup:        sup.StateStopped,
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的宽限窗口内经 -quit 信使请求主实例落盘退出
	// （Everything 官方 CLI 契约，先存索引库再收口），超时由内核强杀兜底。
	e.sup.SetQuitHook(func(context.Context) error {
		exe := e.sup.Snapshot().Exe
		if exe == "" {
			return errors.New("Everything 实例 exe 未登记，跳过 -quit 信使")
		}
		return spawnQuitMessenger(exe)
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running）。
// background 模式追加 -startup（后台驻留不显示窗口）；window 模式无参直接起窗。
// 工作目录锁定到 exe 所在目录由内核默认保证——便携版配置与索引库按相对路径
// 落在 exe 同目录依赖这一点。本方法不做存在性探测：冷启动与外部实例竞速的
// TOCTOU 交给内核 wait 退出分类兜底（ReadyTimeout=0 即 markeron 同款冷启动语义）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.mode = opts.Mode
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：Everything 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（索引、全局热键不受托管影响）。
	var args []string
	if opts.Mode == ModeBackground {
		args = []string{"-startup"}
	}
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          args,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// OpenWindow 完成单实例协议唤起搜索窗口：拉起同路径第二个 Everything 实例充当
// "信使"，单实例协议把二次启动（无参 = 开窗意图）转发给主实例显示窗口；
// 信使拉起成功即报告 opened（主实例在场与否由 service 层依据快照判定）。
// 自有实例自此窗口可见（用户随后关闭窗口也会脱同步——Mode 仅信息提示）。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	if err := e.spawnMessenger(exe); err != nil {
		return false, err
	}

	outer := e.sup.Snapshot()
	var snap Snapshot
	e.mu.Lock()
	if outer.State == sup.StateRunning {
		e.mode = ModeWindow
	}
	snap = e.snapshotLocked(outer)
	e.mu.Unlock()
	e.emit(snap)
	return true, nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 -quit 信使
// → 宽限 quitGracePeriod 内等进程自然收口（先落盘索引库再退出）→ 超时 JobObject
// 强杀兜底。幂等；external 态按既有契约映射为无操作成功（指引文案由 service 层给出）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(quitGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不发 -quit 信使、不耗宽限，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待落盘）。
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

// RefreshExternal 探测窗口类/互斥体双通道校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己（或另一并存的
// 通道实例），会误导状态机。
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

// WaitReady 阻塞等待 Everything 实例就绪（托盘通知窗口类出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForEverythingReady(timeout)
}

// IsSearchWindowOpen 搜索主窗口是否打开——空闲自动退出的豁免信号
// （窗口开着说明用户可能正在其中输入，不能静默退出）。探针领域能力，不经内核。
func (e *Engine) IsSearchWindowOpen() bool {
	return e.probe.IsSearchWindowOpen()
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → everything 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
// 运行模式推算在此收口：external 入账分两类——自有进程退出后探测仍命中
// （1.4/1.5 通道并存竞速接管，上一广播态还是活的）保留最近模式账目；
// 静止态探测发现外部实例（上一广播态已是终态）启停形态不可知，清空不揣测。
func (e *Engine) onSupState(s sup.Snapshot) {
	var snap Snapshot
	e.mu.Lock()
	if s.State == sup.StateExternal && !supStateLive(e.prevSup) {
		e.mode = ""
	}
	e.prevSup = s.State
	snap = e.snapshotLocked(s)
	e.mu.Unlock()
	e.emit(snap)
}

// supStateLive 判断内核广播态是否表示"自有进程账目仍在世"（external 承接自它
// 即为竞速接管，而非静止态新发现）。
func supStateLive(s sup.State) bool {
	switch s {
	case sup.StateRunning, sup.StateStarting, sup.StateStopping:
		return true
	}
	return false
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
		Mode:      e.mode,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（everything 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 everything 既有失败文案：内核异常退出消息改回
// "Everything 异常退出（退出码 N）。请检查索引库是否损坏"；内核手动停止的
// "已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
// 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("Everything 异常退出（退出码 %d）。请检查索引库是否损坏", code)
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

// supProbe 把 EverythingProbe（窗口类双通道：TASKBAR_NOTIFICATION 托盘通知窗口类
// 优先、命名互斥体兜底）适配为内核统一探针契约。探测为瞬时系统调用、天然不可
// 取消（任何失败按"不存在"处理，永不报错），因此不产出 ProcInfo（外部实例归属
// 只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p EverythingProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsEverythingRunning(), nil, nil
}
