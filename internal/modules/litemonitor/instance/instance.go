package instance

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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
	StateExternal State = "external" // 外部用户自启的 LiteMonitor 实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"窗口被 HideMainForm 隐藏后 WM_CLOSE 不可达"等罕见场。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；litemonitor 既有快照口径在 stopped
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
	Version string // 绑定版本 vX.Y.Z
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // LiteMonitor.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("LiteMonitor.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine LiteMonitor 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 LiteMonitor 专属账目（退出码/停止时刻）、首启 settings.json 播种、
// 按 PID 唤窗直操作与 740 提权特判文案的广播收口。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	// spawn 失败广播暂扣账目（740 提权特判的收口窗口）：内核 spawn 失败的原始
	// 文案不含提权指引话术，而 elevateHint 需对 Start 返回的错误链做 errors.As
	// 判级（errno 文本随系统语言变化，不可靠字符串匹配）——Start 在途时命中
	// spawn 失败前缀的 failed 广播暂存此处，待 Start 返回后由 wrapper 改写补播。
	holdSpawnFail   bool
	heldSpawnSnap   sup.Snapshot
	heldSpawnSnapOK bool

	sup   *sup.Engine
	probe LiteMonitorProbe
	cb    Callbacks
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe LiteMonitorProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe: probe,
		cb:    cb,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向自有实例 PID 的全部顶层窗口投递
	// WM_CLOSE（LiteMonitor 退出菜单即 form.Close() 且无 FormClosing 拦截，
	// 关窗即退进程；HideMainForm 隐藏窗 WM_CLOSE 仍可达），
	// 超时由内核 JobObject 强杀兜底——settings.json 上游为 tmp+rename 原子写
	// + .bak 备份，进程级终止安全。
	e.sup.SetQuitHook(func(context.Context) error {
		if pid := e.sup.Snapshot().PID; pid != 0 {
			postCloseByPID(pid)
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——settings.json/themes 按
// BaseDirectory 解析依赖此）。LiteMonitor 无后台启动 CLI，唯一启动语义即
// "无参拉起 → 主监控条显示"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出
// 分类兜底（ReadyTimeout=0 即 markeron 同款冷启动语义——第二实例抢锁失败静默
// exit 0 → 进程枚举命中外部实例 → external 接管）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 首启种子配置：关闭上游内置更新检查（仅 settings.json 不存在时写入，
	// 已有配置一字节不动）。失败不阻断启动，最坏回到上游默认行为。
	// 播种属模块策略、刻意留在本层紧邻 spawn（内核不做启动前干预）。
	_ = seedManagedSettings(filepath.Dir(opts.Exe))

	// spawn 失败广播暂扣开闸（740 特判）：见 Engine holdSpawnFail 注释。
	e.mu.Lock()
	e.holdSpawnFail = true
	e.mu.Unlock()

	// LiteMonitor 是 GUI 子系统程序，无需隐藏控制台窗口，也不做任何窗口干预
	// （零 fork 承诺）。
	err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})

	e.mu.Lock()
	e.holdSpawnFail = false
	held, heldOK := e.heldSpawnSnap, e.heldSpawnSnapOK
	e.heldSpawnSnap, e.heldSpawnSnapOK = sup.Snapshot{}, false
	e.mu.Unlock()
	if heldOK && err != nil {
		// requireAdministrator + 未提权父进程：740 直译为"要求提升"不友好，
		// 改写为指向"以管理员身份运行 Hanxi"的指引文案后补播（词表零漂移）。
		if hint := elevateHint(err); hint != "" {
			held.Error = hint
		}
		e.mu.Lock()
		snap := e.snapshotLocked(held)
		e.mu.Unlock()
		e.emit(snap)
	}
	return err
}

// RestoreWindow 直接唤起自有实例窗口：EnumWindows 按 PID 定位顶层窗口 →
// SW_RESTORE + SetForegroundWindow。LiteMonitor 的第二实例抢互斥体失败
// 静默退出、无唤窗回调（Program.cs 实证），所以"打开窗口"不能走信使，
// 只能直操作窗口。此决策理由请勿在后续维护中"好心"改回二次拉起。
func (e *Engine) RestoreWindow() {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.PID == 0 {
		return
	}
	restoreWindowByPID(s.PID)
	e.emit(e.Snapshot())
}

// RestoreExternalWindow 唤起外部实例窗口（外部 PID 未知，由探测枚举提供）。
// 与自有实例同走 Win32 直操作路径（上游契约对"谁拉起的不区分"）。
func (e *Engine) RestoreExternalWindow(pids []uint32) {
	for _, pid := range pids {
		restoreWindowByPID(pid)
	}
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 按 PID 投递
// WM_CLOSE → 宽限 closeGracePeriod 内等进程自然收口（关窗即退场）→ 超时
// JobObject 强杀兜底（窗口未创建/模态对话框阻塞消息循环等场）。
// 幂等；external 态按既有契约映射为无操作成功（指引文案由 service 层给出）。
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
			return nil // external 状态不在管辖范围内：强杀越权，收口由 service 层指引
		}
		return err
	}
	return nil
}

// RefreshExternal 进程枚举校正 external/stopped 状态（委托内核）。
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

// WaitReady 阻塞等待 LiteMonitor 实例就绪（进程出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 自有实例的可见顶层窗口是否存在——边缘自动隐藏/HideMainForm
// 场返回 false（其监控条常态可见，此信号用于前端"窗口已隐藏，点击唤起"提示）。
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

// ---------- 内核 → litemonitor 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
// spawn 失败广播在 Start 在途时暂扣（740 提权特判的收口窗口），其余照常映射转发。
func (e *Engine) onSupState(s sup.Snapshot) {
	e.mu.Lock()
	if e.holdSpawnFail && isSpawnFailure(s) {
		e.heldSpawnSnap, e.heldSpawnSnapOK = s, true
		e.mu.Unlock()
		return
	}
	snap := e.snapshotLocked(s)
	e.mu.Unlock()
	e.emit(snap)
}

// isSpawnFailure 判定内核快照是否为"进程创建/启动失败"类消息（spawn 路径的
// failed 广播）。措辞耦合自 supervisor.Start 的两处 transition 前缀——内核文案
// 变更时本判定退化为不暂扣（原始透传广播照常送达），仅丢失 740 改写层，不崩溃。
func isSpawnFailure(s sup.Snapshot) bool {
	return s.State == sup.StateFailed &&
		(strings.HasPrefix(s.Error, "进程启动失败: ") || strings.HasPrefix(s.Error, "进程创建失败: "))
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
//	supervisor stopping → running（litemonitor 既有词表无 stopping：终止窗口对前端
//	                      保持运行语义，收口后由 stopped/failed 终态广播纠正）
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

// mapErrorMessage 还原 litemonitor 既有失败文案：
//   - 内核异常退出消息改回"LiteMonitor 异常退出（退出码 N）。请确认已安装
//     .NET 8 桌面运行时，或尝试重新安装该版本"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("LiteMonitor 异常退出（退出码 %d）。请确认已安装 .NET 8 桌面运行时，或尝试重新安装该版本", code)
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

// supProbe 把 LiteMonitorProbe（进程快照枚举）适配为内核统一探针契约。
// LiteMonitor 的单实例互斥体名随安装路径派生、外部实例不可复现（见 prober.go），
// 进程名 LiteMonitor.exe 是唯一稳定标识；与 markeron/ccswitch 的互斥体探针不同，
// 本探针能给出存活 PID——Inspect 附带首个命中进程的 ProcInfo，内核 external
// 入账时据此充实快照 PID。快照枚举为瞬时系统调用、天然不可取消（任何失败按
// "不存在"处理，永不报错）。
type supProbe struct{ p LiteMonitorProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	pids := s.p.FindPIDs()
	if len(pids) == 0 {
		return false, nil, nil
	}
	return true, &platform.ProcInfo{PID: pids[0], Name: exeName}, nil
}
