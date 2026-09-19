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
	StateExternal State = "external" // 外部用户自启的 PicLite 主实例（非本引擎托管）
)

// quitGraceWindow Quit 交给内核 Stop 的 grace 预算。PicLite 的 QuitHook 恒即时
// 返错（无优雅通道，见 NewEngine 注释），预算实际不会被消耗；保留非零值是让
// 内核确实咨询钩子——grace==0 会跳过钩子调用，"通道不存在"的显式声明就退化
// 成永不执行的死代码。
const quitGraceWindow = 2 * time.Second

// errNoGracefulQuit PicLite 优雅退出通道不存在的显式声明：QuitHook 恒返本错误，
// 按 supervisor 语义（"钩子返回错误时直接进入强制终止"），Quit 收敛为
// JobObject 直接强杀。刻意不采用"不注册钩子"的写法——两者内核行为等价（都强杀），
// 但注册即时返错的钩子把"无通道"从缺省沉默变成显式事实源：读代码即知这不是
// 漏配，上游若将来补出优雅通道（如 CLI 参数），只需替换本钩子实现。
//
// 上游实证（勿回退照抄 ccswitch 模板）：
//   - 主窗口 WM_CLOSE 被 prevent_close+hide 拦下（关窗只藏进托盘，不退进程）；
//     ExitRequested 未置 quitting 标志也被 prevent_exit 拦截，且无 -quit 类 CLI、
//     无命名管道命令词表；
//   - 向单实例消息窗口投 WM_CLOSE 是净损害：DefWindowProc 直接 DestroyWindow，
//     既不触发退出，又拆掉单实例协议载体（后续信使失联）。
var errNoGracefulQuit = errors.New("piclite: 上游无优雅退出通道（关窗只藏进托盘、WM_CLOSE 会拆除单实例协议载体），直接强制终止")

// manualStopWording 内核手动停止的收口文案；piclite 既有快照口径在 stopped
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
	Exe      string // piclite.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("piclite.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine PicLite 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 PicLite 专属账目（退出码/停止时刻）、"无优雅通道"QuitHook 声明
// 与信使唤窗。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe PicLiteProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe PicLiteProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 无优雅退出通道的显式声明：钩子即时返错 → 内核 Stop 跳过 grace 窗口，
	// 直接走 killSequence（JobObject Terminate 优先，兜底强杀）；
	// PicLite 配置（app-profile/upload-profile JSON）为前端修改即写盘，
	// 进程级终止不丢设置（进行中的批量压缩中断，与上游自身崩溃等价，可接受）。
	e.sup.SetQuitHook(func(context.Context) error {
		return errNoGracefulQuit
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证）。
// PicLite 唯一启动语义即"无参拉起 → 主窗口显示"（--minimized 自启参数刻意
// 不用：托管入口是"打开窗口"，带参启动反而需要二次信使唤窗竞态）。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类
// 兜底（ReadyTimeout=0 即 markeron 同款冷启动语义；就绪等待由 service 层经
// WaitReady 另行执行）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：PicLite 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// OpenWindow 完成单实例协议的窗口唤起：拉起同路径第二个 PicLite 实例充当
// "信使"，tauri-plugin-single-instance 回调无条件 show+focus 主窗口（见
// messenger.go 的拉起纪律）；主实例是否存在由 service 层依据快照判定。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	if err := e.spawnMessenger(exe); err != nil {
		return false, err
	}
	e.emit(e.Snapshot())
	return true, nil
}

// Quit 退出引擎托管的 PicLite（幂等；external/stopped 状态无自有进程，按既有
// 契约映射为无操作成功）。与 ccswitch 模板的关键差异——直接强杀（同 keyviz）：
// QuitHook 即时返错使内核跳过 grace 窗口进入强制终止（理由与数据安全论证见
// errNoGracefulQuit 与包注释）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(quitGraceWindow)
}

// Stop 立即强杀自有实例（幂等，grace=0 连钩子都不咨询）。与 Quit 语义在
// PicLite 处收敛为同一路径（都是 JobObject 终止），保留两个入口维持家族 API
// 一致（应用退出 Shutdown 通道用）。
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

// WaitReady 阻塞等待 PicLite 实例就绪（单实例互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsUserWindowOpen 自有实例是否有可见用户窗口——空闲自动退出的豁免信号。
// 按自有进程 PID 判定（探针领域能力，不经内核）；非 running 恒为 false。
func (e *Engine) IsUserWindowOpen() bool {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning {
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

// ---------- 内核 → piclite 形状映射 ----------

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
//	supervisor stopping → running（piclite 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 piclite 既有失败文案：
//   - 内核异常退出消息改回"PicLite 异常退出（退出码 N）。请确认已安装
//     WebView2 Runtime"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("PicLite 异常退出（退出码 %d）。请确认已安装 WebView2 Runtime", code)
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

// supProbe 把 PicLiteProbe（命名互斥体存在性）适配为内核统一探针契约。
// 互斥体探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 无从取得，沿用原口径；
// 空闲豁免所需的 PID 由 running 态自有进程经 IsUserWindowOpen 独立取得）。
type supProbe struct{ p PicLiteProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
