package instance

import (
	"context"
	"errors"
	"fmt"
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
	StateRunning  State = "running"  // 自有 Job 托管主实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 Rufus 实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"写盘确认对话框挂住消息循环无人点"等罕见场（风险说明见包注释）。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

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
	Version string // 绑定版本 vX.Y
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // rufus.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("rufus.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Rufus 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 Rufus 专属适配（便携 ini 播种、WM_CLOSE 唤退信令、UAC 提权指引
// 文案、退出码/停止时刻账目）。
type Engine struct {
	mu          sync.Mutex
	exitCode    int       // 自有实例最近一次退出码（Start 时清零）
	stoppedAt   time.Time // 自有实例最近一次落终态的时刻
	errOverride string    // spawn 失败时按 740 提权指引覆盖内核原始文案（Start 时清零）

	sup   *sup.Engine
	probe RufusProbe
	cb    Callbacks

	// specArgs 真机冒烟专用注入缝：生产 Rufus 恒无参拉起（nil，无参即开主
	// 对话框），测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能稳定
	// 存活的确定性命令参数。
	specArgs []string
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe RufusProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe: probe,
		cb:    cb,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// Quit 的优雅通道：向自有实例全部顶层窗口投递 WM_CLOSE（Rufus 关主对话框
	// 即退进程）。上游无 CLI 退出信使，窗口消息是唯一优雅手段；内核在投递成功
	// 后于 grace 窗口内等待自然退出，超时落入 JobObject 强杀兜底。
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

// Start 启动自有实例：播种便携配置 → 委托内核（创建进程 → 绑定 JobObject →
// running）。Rufus 无后台启动 CLI，唯一启动语义即"无参拉起 → 主对话框显示"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类
// 兜底（第二实例抢锁失败弹模态错误框退出 → 进程枚举/互斥体仍命中外部实例 →
// external，ReadyTimeout=0 即内核文档所述冷启动语义）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.errOverride = ""
	e.mu.Unlock()

	// 首启种子配置：激活便携模式并关闭上游内置更新检查（仅 rufus.ini 不存在时
	// 写入）。失败不阻断启动，最坏回到上游默认行为（设置落注册表 + 弹更新检查）。
	_ = SeedPortableSettings(filepath.Dir(opts.Exe))

	// 刻意不设置 HideWindow 等窗口干预：Rufus 是 GUI 子系统对话框应用，
	// 既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
	err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
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

// RestoreWindow 直接唤起自有实例窗口：EnumWindows 按 PID 定位顶层窗口 →
// SW_RESTORE + SetForegroundWindow。Rufus 的第二实例抢互斥体失败会弹出
// 系统模态"已在运行"错误框再退出（rufus.c 实证），二次拉起不但唤不了窗
// 还要用户多点一次对话框——"打开窗口"绝不能走信使，只能直操作窗口。
// 此决策理由请勿在后续维护中"好心"改回二次拉起。
func (e *Engine) RestoreWindow() {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.PID == 0 {
		return
	}
	restoreWindowByPID(s.PID)
	e.emit(e.snapshotOf(s))
}

// RestoreExternalWindow 唤起外部实例窗口（外部 PID 未知，由探测枚举提供）。
func (e *Engine) RestoreExternalWindow(pids []uint32) {
	for _, pid := range pids {
		restoreWindowByPID(pid)
	}
}

// Quit 退出自有实例（幂等；external/stopped 状态无自有进程，内核回 ErrExternal
// 或幂等 nil，按 Rufus 既有契约均映射为无操作成功）：委托内核 Stop(grace)——
// quitHook 投递 WM_CLOSE → 宽限 closeGracePeriod 等自然退出 → 超时 JobObject
// 强杀兜底。⚠ 若此刻正在写盘，强杀会产出半成品盘；正常路径用户在窗口内自己
// 确认，前端常态展示"写入中勿退出"警示条（见 RufusView）。
func (e *Engine) Quit() error {
	return e.stopWith(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不做 WM_CLOSE 优雅退出，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待对话框流程）。
func (e *Engine) Stop() error {
	return e.stopWith(0)
}

// stopWith 统一停止通道：grace>0 时先走 quitHook 优雅投递再强制兜底；
// external 状态不在管辖范围内（内核回 ErrExternal）→ 映射为无操作成功，
// 指引文案由 service 层给出。
func (e *Engine) stopWith(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil
		}
		return err
	}
	return nil
}

// RefreshExternal 探测外部实例校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.sup.RefreshExternal()
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	return e.snapshotOf(e.sup.Snapshot())
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	return s.Exe
}

// WaitReady 阻塞等待 Rufus 实例就绪（进程出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 自有实例的可见顶层窗口是否存在——Rufus 常态有窗，
// 此信号用于前端"进程在但窗口找不到"的边缘提示（如模态子对话框阻塞态）。
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

// ---------- 内核 → rufus 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
func (e *Engine) onSupState(s sup.Snapshot) {
	e.emit(e.snapshotOf(s))
}

// snapshotOf 内核快照 + 本包推算账目 → Rufus Snapshot。
func (e *Engine) snapshotOf(s sup.Snapshot) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := mapState(s.State)
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
		State:     state,
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
//	supervisor stopping → running（rufus 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 rufus 既有失败文案（前置条件：已持 e.mu 或由 snapshotOf
// 调用）：spawn 提权失败覆盖为 740 指引；内核异常退出消息改回
// "Rufus 异常退出（退出码 N）。若窗口从未出现…"；手动停止的"已手动停止"
// 按 rufus 既有口径清空（markeron 金样本相反——它历史就在传该文案）。
// 其余（启动失败等）透传。
func (e *Engine) mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if e.errOverride != "" {
			return e.errOverride
		}
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("Rufus 异常退出（退出码 %d）。若窗口从未出现，请确认 Hanxi 正以管理员身份运行（Rufus 清单强制提权），或尝试重新安装该版本", code)
		}
	}
	if s.State == sup.StateStopped && s.Error == "已手动停止" {
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

// supProbe 把 RufusProbe（互斥体 + 进程枚举）适配为内核统一探针契约。
// 探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 由 ExternalPIDs
// 单独取用，沿用原口径）。
type supProbe struct{ p RufusProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
