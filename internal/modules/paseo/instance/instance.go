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
	StateExternal State = "external" // 外部用户自启的 Paseo 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// Paseo 关闭要走 daemon 清理生命周期（before-quit 收敛内置 daemon 与其拉起的
// agent/PTY 进程树），比裸 Electron 应用重一档，取 8s（recordly 为 3s）。
// 包级变量仅为单测可压缩等待时长，生产值保持 8s。
var closeGracePeriod = 8 * time.Second

// externalSettle wait 判外部接管前的静默期：进程名探测是进程树级的，
// 外层 Electron 主进程刚退出时 Chromium 子进程/helper 需要短暂时间随之消亡，
// 立即探测会把"自己刚退"误判成"外部实例还在"。由 supProbe 在内核退出分类
// 探针调用前落地（仅对受管在途探针生效，静止态校正保持即时）。包级变量供
// 单测压缩；生产 500ms 足够覆盖 Chromium 子进程收敛（recordly 同参数）。
var externalSettle = 500 * time.Millisecond

// manualStopWording 内核手动停止的收口文案；paseo 既有快照口径在 stopped
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
// 刻意无 Env 注入通道：Paseo 的共享数据 + 单锁组语义不靠 env 实现，上游也
// 无自动更新禁用开关（源码实证无 PASEO_DISABLE_UPDATES 类变量），注入即虚构
// 契约——版本升级统一引导走 Hanxi 版本管理，提示条如实标注。
type StartOptions struct {
	Version  string // 绑定版本 0.8.0（beta 含 -beta.N 后缀）
	Exe      string // Paseo.exe 绝对路径（托管版本目录内）
	Detached bool   // 独立运行：解除 JobObject 退出联动（Hanxi 关闭不影响工具）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Paseo.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Paseo 单实例运行引擎：组合内核 supervisor.Engine，
// 本层只保留 Paseo 领域适配（进程名+窗口探针、Win32 直唤/信使兜底两条
// 唤窗通道、WM_CLOSE 优雅退出钩子、状态词表与快照形状映射、
// 退出分类探针的树级静默期 externalSettle）。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe PaseoProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe PaseoProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe: probe, e: e}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向 Paseo 可见窗口投递 WM_CLOSE
	// （上游 Windows 无托盘，主窗口关闭 → window-all-closed → app.quit()，
	// before-quit 完成 daemon 清理），超时由内核 JobObject 强杀兜底。
	e.sup.SetQuitHook(func(context.Context) error {
		postClose()
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证）。
// Paseo 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类
// 兜底（我方第二实例拿不到锁会自退，探测到进程树仍在 = 外部主实例接管，
// ReadyTimeout=0 即 markeron 同款冷启动语义）。
//
// 进程治理收口说明（daemon/PTY 树）：Electron 主进程与其派生的内置 daemon
// （ELECTRON_RUN_AS_NODE 复用同一镜像）、agent/PTY 子进程全部落在同一
// JobObject（Create 即 KILL_ON_JOB_CLOSE、无 breakaway 许可，子进程自动继承），
// 整树终止归 Job：Stop/Quit 的 job.Terminate 一次收口全树，Hanxi 崩溃退出
// 句柄关闭同样连带全树——迁移前后语义不变。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：Paseo 是 GUI 子系统程序（Electron），
	// 既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// FocusWindow 唤起已运行实例的可见窗口（Win32 直唤，自有或外部均可——共享数据
// 下全局至多一个桌面主实例，无归属歧义）。返回 false = 进程在但无可见窗，
// 调用方才可退到 OpenMessenger 请求开新窗（上游 second-instance 语义是
// openAdditional 新开窗口而非聚焦，唤窗优先直唤、信使仅兜底——两条通道
// 都属 Paseo 领域策略，留在本包不经内核）。
func (e *Engine) FocusWindow() bool {
	return e.probe.FocusWindow()
}

// OpenMessenger 拉起"信使"二次进程请求开新窗（见 messenger.go 的拉起纪律）：
// 仅在无可见窗口时作为兜底通道使用（有窗时唤窗一律走 FocusWindow，避免
// "点一次开一扇新窗"的直觉反差）。
func (e *Engine) OpenMessenger(exe string) error {
	if err := e.spawnMessenger(exe); err != nil {
		return err
	}
	e.emit(e.Snapshot())
	return nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 WM_CLOSE
// → 宽限 closeGracePeriod 内等进程自然收口（daemon 清理生命周期）→ 超时
// JobObject 强杀兜底（daemon 账本为文件持久化，进程级终止风险收敛到会话
// 中断，不毁配置）。幂等；external 态按既有契约映射为无操作成功。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止整树（应用退出时 Shutdown 通道用，无需等待 daemon 清理）。
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

// RefreshExternal 探测进程名+窗口校正 external/stopped 状态（委托内核）。
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

// WaitReady 阻塞等待 Paseo 实例就绪（主窗口出现），超时返回 false。
// 探针领域能力不经内核（Electron 冷启动 + 内置 daemon 拉起，就绪信号是窗口
// 而非进程在场；service 层以 readyTimeout=45s 调用）。
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

// ---------- 内核 → paseo 形状映射 ----------

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
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			e.exitCode = code
		}
	}
	switch s.State {
	case sup.StateStopped, sup.StateFailed:
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
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
//	supervisor stopping → running（paseo 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 paseo 既有失败文案：
//   - 内核异常退出消息改回"Paseo 异常退出（退出码 N）。若刚拉起即退出…"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传——内核 Job 链路失败文案（"创建 Job Object 失败
//     …"等）与原实现口径同源。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf(
				"Paseo 异常退出（退出码 %d）。若刚拉起即退出，请检查杀毒软件是否拦截未签名程序，或先退出正在运行的 Paseo 实例后重试", code)
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

// supProbe 把 PaseoProbe（进程名 Paseo.exe + EnumWindows 树级信号）适配为
// 内核统一探针契约。探测为瞬时系统调用、天然不可取消（任何失败按"不存在"
// 处理，永不报错），因此不产出 ProcInfo（外部实例归属只认 running 事实，
// PID 无从取得，沿用原口径）。
//
// externalSettle 落地纪律：仅当内核仍持受管进程（starting/running 态的探针
// 调用必来自 wait 退出分类或 awaitReady——静止态的 RefreshExternal/classify
// 均不满足本条件）时先静默 externalSettle 再探测，与迁移前 wait 口径逐拍
// 对齐："外层主进程退出但 daemon 残树仍在"→ 外部接管 external，绝不因
// helper 随主进程消亡的短暂滞后而误判。
type supProbe struct {
	probe PaseoProbe
	e     *Engine
}

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	if st := s.e.sup.Snapshot().State; st == sup.StateRunning || st == sup.StateStarting {
		time.Sleep(externalSettle)
	}
	return s.probe.IsRunning(), nil, nil
}
