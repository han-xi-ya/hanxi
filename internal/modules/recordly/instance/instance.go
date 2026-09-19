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
	StateExternal State = "external" // 外部用户自启的 Recordly 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"编辑器多窗口收起回 HUD"等非一步退出的场（上游 Windows 无托盘，
// 主窗口关闭即 app.quit，正常单窗口在宽限内完成）。
// 包级变量仅为单测可压缩等待时长，生产值保持 3s（Electron 关闭要跑
// before-quit 清理：杀采集子进程、还原光标、收导出流，比原生应用慢一档）。
var closeGracePeriod = 3 * time.Second

// externalSettle wait 判外部接管前的静默期：进程名探测是进程树级的，
// 自有主进程刚退出时 Electron 子进程（渲染/GPU/helper，全部同名 Recordly.exe）
// 需要短暂时间随之消亡，立即探测会把"自己刚退"误判成"外部实例还在"。
// 由 supProbe 在内核退出分类探针调用前落地（仅对受管在途探针生效，静止态
// 校正保持即时）。包级变量供单测压缩；生产 500ms 足够覆盖 Chromium 子进程
// 收敛（paseo 同族同参数）。
var externalSettle = 500 * time.Millisecond

// manualStopWording 内核手动停止的收口文案；recordly 既有快照口径在 stopped
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
	Version string   // 绑定版本 vX.Y.Z（beta 通道含 -beta.N 后缀）
	Exe     string   // Recordly.exe 绝对路径（托管安装目录内）
	Env     []string // 追加环境变量（托管关键项 RECORDLY_DISABLE_AUTO_UPDATES=1
	// 由 service 层注入：electron-updater 的 quitAndInstall 会按注册表覆写安装
	// 目录，打乱 Hanxi 版本管理；上游 updater.ts 实证官方 env 开关。经内核
	// Spec.Env 受控字段落地"继承父环境 + 追加"语义，本包不再自带合并实现）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Recordly.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Recordly 单实例运行引擎：组合内核 supervisor.Engine，
// 本层只保留 Recordly 领域适配（进程名+EnumWindows 树级探针、WM_CLOSE 优雅
// 退出钩子、信使唤窗、状态词表与快照形状映射、退出分类探针的树级静默期
// externalSettle、退出码/停止时刻账目）。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe RecordlyProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）

	// specArgs 真机冒烟专用注入缝：生产 Recordly 恒无参拉起（nil），
	// 测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能稳定存活。
	specArgs []string
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe RecordlyProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe: probe, e: e}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向 Recordly 可见窗口投递 WM_CLOSE
	// （上游 Windows 无托盘，主窗口关闭 → window-all-closed → app.quit()，
	// before-quit 完成采集子进程清理），超时由内核 JobObject 强杀兜底。
	e.sup.SetQuitHook(func(context.Context) error {
		postClose()
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证）。
// Recordly 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类
// 兜底（ReadyTimeout=0 即 markeron 同款冷启动语义——我方第二实例拿不到锁会
// exit 0 自退，静默收敛后探测到进程树仍在 = 外部主实例接管）。
//
// 进程治理收口说明（Electron 树）：主进程与其渲染/GPU/原生采集 helper
// （wgc-capture、cursor-monitor 等，全部继承同名镜像）落在同一 JobObject
// （KILL_ON_JOB_CLOSE、无 breakaway 许可），整树终止归 Job：Stop/Quit 的
// job.Terminate 一次收口全树，Hanxi 崩溃退出句柄关闭同样连带全树——
// 迁移前后语义不变。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：Recordly 是 GUI 子系统程序（Electron），
	// 既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		Env:           opts.Env,      // RECORDLY_DISABLE_AUTO_UPDATES=1 托管注入通道
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// OpenWindow 完成单实例协议的窗口唤起：已有主实例（自有/外部）时拉起同路径
// "信使"进程，Electron second-instance 回调唤起主窗口（见 messenger.go 的
// 拉起纪律）；无实例时由 service 层直接走 Start——信使属领域策略不经内核 Engine。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	if err := e.spawnMessenger(exe); err != nil {
		return false, err
	}
	e.emit(e.Snapshot())
	return true, nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 WM_CLOSE
// → 宽限 closeGracePeriod 内等进程自然收口（before-quit 清理采集子进程、
// 还原光标、收导出流）→ 超时 JobObject 强杀兜底（编辑器多窗口收起、未保存
// 弹窗等；项目保存上游为原子写 #741，进程级终止风险收敛到最小）。
// 幂等；external 态按既有契约映射为无操作成功。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止整树（应用退出时 Shutdown 通道用，无需等待清理动画）。
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

// WaitReady 阻塞等待 Recordly 实例就绪（主窗口出现），超时返回 false。
// 探针领域能力不经内核（Electron 冷启动慢，就绪信号是窗口而非进程在场）。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen Recordly 是否有可见窗口——空闲自动退出的豁免信号
// （录制 HUD 浮层在场同样命中：录制中绝不空闲退出）。
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

// ---------- 内核 → recordly 形状映射 ----------

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
//	supervisor stopping → running（recordly 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 recordly 既有失败文案：
//   - 内核异常退出消息改回本引擎既有话术（未签名安装器 + 杀软拦截是真实
//     高发成因，真机首撞者靠它自查）；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传——内核 Job 链路失败文案（"创建 Job Object 失败
//     …"等）与原实现口径同源。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("Recordly 异常退出（退出码 %d）。其安装器未数字签名，若刚拉起即退出请检查杀毒软件是否拦截了未签名程序", code)
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

// supProbe 把 RecordlyProbe（进程名 Recordly.exe + EnumWindows 树级信号）适配为
// 内核统一探针契约。Toolhelp32 快照为瞬时系统调用、天然不可取消（任何失败按
// "不存在"处理，永不报错），因此不产出 ProcInfo（外部实例归属只认 running
// 事实，进程名探测拿不到可信主进程 PID，沿用原口径）。
//
// externalSettle 落地纪律：仅当内核仍持受管进程（starting/running 态的探针
// 调用必来自 wait 退出分类或 awaitReady——静止态的 RefreshExternal/classify
// 均不满足本条件）时先静默 externalSettle 再探测，与迁移前 wait 口径逐拍
// 对齐："自有主进程刚退但 Electron helper 残树仍在"→ 外部接管 external，
// 绝不因 helper 随主进程消亡的短暂滞后而误判。
type supProbe struct {
	probe RecordlyProbe
	e     *Engine
}

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	if st := s.e.sup.Snapshot().State; st == sup.StateRunning || st == sup.StateStarting {
		time.Sleep(externalSettle)
	}
	return s.probe.IsRunning(), nil, nil
}
