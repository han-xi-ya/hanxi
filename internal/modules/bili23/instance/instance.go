package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"hanxi/internal/platform"
)

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（极短窗口）
	StateRunning  State = "running"  // 自有 Job 托管主实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 Bili23 主实例（非本引擎托管）
)

// QuitResult Quit 的三态结果：上游关窗行为用户可配（EXIT/MINIMIZE/ALWAYS_ASK），
// WM_CLOSE 后进程是否退出取决于用户设置与用户在询问对话框中的选择。
type QuitResult string

const (
	// QuitExited 进程已在宽限内退出（WhenClose=EXIT，或询问对话框被选了"退出"）。
	QuitExited QuitResult = "exited"
	// QuitHidden 进程存活且无可见窗口：WhenClose=MINIMIZE 的收入托盘态，
	// 或被强杀前用户又唤起了窗口之外的静默期。托管关系保留，可唤窗、可强制结束。
	QuitHidden QuitResult = "hidden"
	// QuitWindowUp 进程存活且仍有可见窗口：询问对话框正等待用户选择（或用户取消后
	// 窗口恢复显示）。不替用户做决定，不强杀。
	QuitWindowUp QuitResult = "windowUp"
)

var (
	// closeGracePeriod Quit 第一阶段：WM_CLOSE 投递后观察进程是否即刻退出的宽限。
	// 包级变量仅为单测可压缩等待时长，生产值保持 3s。
	closeGracePeriod = 3 * time.Second
	// cleanupGracePeriod Quit 第二阶段：窗口已不可见但进程存活时的收尾观察期。
	// 上游 closeEvent 退出前会等下载线程收敛、任务队列落盘（downloader_manager.shutdown
	// + AsyncTask.safe_quit + task_manager.shutdown），在途任务多时耗时以十秒计，
	// 给足上限避免把"正在优雅退出"误判为"驻留托盘"进而怂恿用户强杀。
	cleanupGracePeriod = 25 * time.Second
)

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
	Exe      string // Bili23.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Bili23.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Bili23 Downloader 单实例运行引擎。
type Engine struct {
	mu        sync.Mutex
	state     State
	version   string
	pid       uint32
	exitCode  int
	errMsg    string
	external  bool
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止/退出标记：防止进程终止后误判为异常退出

	startMu sync.Mutex // Start/Quit 互斥临界区

	cmd    *exec.Cmd
	job    platform.Job
	jobAPI platform.JobAPI
	probe  Bili23Probe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe Bili23Probe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// Bili23 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给 wait() 退出分类兜底。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.version = opts.Version
	e.external = false
	e.stopping = false
	e.mu.Unlock()

	e.transition(StateStarting, "")

	// Bili23 是 GUI 子系统程序（pythonw 改壳），无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定：_pystand_static.int 引导脚本按 __file__ 相对路径加载 script/
	if err := cmd.Start(); err != nil {
		e.transition(StateFailed, "进程启动失败: "+err.Error())
		return err
	}

	e.mu.Lock()
	e.cmd = cmd // 立即登记
	e.pid = uint32(cmd.Process.Pid)
	e.exitCode = 0
	e.errMsg = ""
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	job, jerr := e.jobAPI.Create()
	if jerr != nil {
		_ = cmd.Process.Kill()
		go e.wait()
		e.transition(StateFailed, "创建 Job Object 失败: "+jerr.Error())
		return fmt.Errorf("创建 Job Object 失败: %w", jerr)
	}
	if aerr := job.Assign(e.pid); aerr != nil {
		job.Close()
		_ = cmd.Process.Kill()
		go e.wait()
		e.transition(StateFailed, "JobObject 绑定失败: "+aerr.Error())
		return fmt.Errorf("JobObject 绑定失败: %w", aerr)
	}
	if opts.Detached {
		// 解除退出联动：Hanxi 退出/崩溃不再连带杀本实例（"不随 Hanxi 关闭"开关）
		if derr := job.SetAllowKillOnClose(false); derr != nil {
			job.Close()
			_ = cmd.Process.Kill()
			go e.wait()
			e.transition(StateFailed, "解除退出联动失败: "+derr.Error())
			return fmt.Errorf("解除退出联动失败: %w", derr)
		}
	}

	e.mu.Lock()
	e.job = job
	e.mu.Unlock()

	go e.wait()
	e.transition(StateRunning, "")
	return nil
}

// OpenWindow 完成单实例协议的窗口唤起：
//   - 已有主实例（自有/外部）→ 无参拉起"信使"进程，第二实例经 QLocalServer
//     写入 activate 命令唤醒主窗口后自行退出（实证自上游 src/main.py）；
//   - 无实例 → 返回 false 由 service 层直接走 Start。
//
// 信使进程 Start 后立即 Release、刻意不 Wait（避免阻塞 RPC）、不进 Job
// （不属于托管生命周期）。此决策理由请勿在后续维护中"好心"改成 Wait。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()

	e.mu.Lock()
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
	return true, nil
}

// Quit 尽力优雅退出自有实例（幂等；external/stopped 状态无自有进程，返回 QuitExited 由
// service 层按快照状态先行分流）：
//  1. 向主实例窗口投递 WM_CLOSE——上游 closeEvent 按用户的"关闭窗口"设置分派：
//     EXIT 直接走收尾退出；MINIMIZE 收入托盘；ALWAYS_ASK 弹出询问对话框（默认）；
//  2. 第一阶段宽限 closeGracePeriod：即刻退出者（EXIT 且无在途任务）在此收敛；
//  3. 仍存活且有可见窗口 → QuitWindowUp（弹窗询问中，等用户选择，绝不代劳）；
//  4. 仍存活且无窗口 → 进入 cleanupGracePeriod 观察：下游收尾（停下载线程、落盘任务
//     队列）可能仍在进行，退出则归 QuitExited；观察期满仍存活才是真的驻留托盘 →
//     QuitHidden。
//
// 与 ccswitch/everything 的本质区别：**任何阶段都不做强杀兜底**——这是下载器，
// 静默终止在途任务不可接受；强杀唯一入口是用户显式调用 Stop（「强制结束」按钮）。
func (e *Engine) Quit() (QuitResult, error) {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, pid := e.state, e.pid
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return QuitExited, nil
	}

	// 1. 尽力优雅（窗口不存在时静默 no-op）
	if pid != 0 {
		postCloseTo(pid)
	}

	e.mu.Lock()
	e.stopping = true // 标记：其后 wait() 收尾归类为"手动退出"
	e.mu.Unlock()

	// 2. 第一阶段宽限
	if e.waitForExit(closeGracePeriod) {
		return QuitExited, nil
	}

	e.mu.Lock()
	alive := e.state == StateRunning || e.state == StateStarting
	e.mu.Unlock()
	if !alive {
		return QuitExited, nil
	}

	// 3. 弹窗询问中：把决定权留给用户
	if e.probe.HasVisibleWindow(pid) {
		e.clearStopping() // 用户可能取消并继续使用：恢复异常退出可被识别为 failed
		return QuitWindowUp, nil
	}

	// 4. 无窗口存活：区分"正在收尾"与"驻留托盘"
	if e.waitForExit(cleanupGracePeriod) {
		return QuitExited, nil
	}
	e.mu.Lock()
	alive = e.state == StateRunning || e.state == StateStarting
	e.mu.Unlock()
	if !alive {
		return QuitExited, nil
	}
	e.clearStopping() // 同上：托管关系保留，后续自然退出/崩溃仍需正确分类
	return QuitHidden, nil
}

// clearStopping 撤销 stopping 标记（Quit 未实际终结进程时恢复退出分类的常态语义）。
func (e *Engine) clearStopping() {
	e.mu.Lock()
	e.stopping = false
	e.mu.Unlock()
}

// waitForExit 轮询直至自有进程退出（wait() 已收尾）或超时；返回是否已退出。
func (e *Engine) waitForExit(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		stillRunning := e.state == StateRunning || e.state == StateStarting
		e.mu.Unlock()
		if !stillRunning {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不做 WM_CLOSE 优雅退出，
// 直接 JobObject 终止（在途下载由上游断点续传兜底；应用退出时 Shutdown 通道用）。
func (e *Engine) Stop() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()
	return e.forceKill(cmd)
}

// forceKill JobObject 强杀自有实例（仅显式「强制结束」/应用退出通道使用）。
func (e *Engine) forceKill(cmd *exec.Cmd) error {
	e.mu.Lock()
	job := e.job
	e.mu.Unlock()
	if job != nil {
		return job.Terminate(1)
	}
	if cmd != nil && cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return fmt.Errorf("实例没有可终止的进程")
}

// RefreshExternal 探测外部实例校正 external/stopped 状态。
// 仅对静止态生效：running/starting 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateStopped && state != StateFailed && state != StateExternal {
		return
	}

	running := e.probe.IsRunning()

	e.mu.Lock()
	var snap Snapshot
	changed := false
	switch {
	case running && e.state != StateExternal:
		e.state = StateExternal
		e.external = true
		e.pid = 0
		e.errMsg = ""
		changed = true
	case !running && e.state == StateExternal:
		e.state = StateStopped
		e.external = false
		e.stoppedAt = time.Now()
		changed = true
	}
	if changed {
		snap = e.snapshotLocked()
	}
	e.mu.Unlock()
	if changed {
		e.emit(snap) // 无变化不广播
	}
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// Exe 返回当前自有实例的可执行路径（非 running 时为空串）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// WaitReady 阻塞等待 Bili23 实例就绪（单实例互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsWindowVisible 自有实例是否有可见顶层窗口（状态灯"运行中·窗口开/收入托盘"提示信号）。
func (e *Engine) IsWindowVisible() bool {
	e.mu.Lock()
	pid := e.pid
	e.mu.Unlock()
	return e.probe.HasVisibleWindow(pid)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateRunning || e.startedAt.IsZero() {
		return 0
	}
	return time.Since(e.startedAt)
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked() Snapshot {
	return Snapshot{
		Version:   e.version,
		State:     e.state,
		PID:       e.pid,
		ExitCode:  e.exitCode,
		Error:     e.errMsg,
		External:  e.external,
		StartedAt: e.startedAt,
		StoppedAt: e.stoppedAt,
	}
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// transition 切换状态并广播。
func (e *Engine) transition(s State, errMsg string) {
	var snap Snapshot
	e.mu.Lock()
	e.state = s
	e.errMsg = errMsg
	switch s {
	case StateRunning:
		e.stoppedAt = time.Time{}
		e.errMsg = ""
	case StateStopped, StateFailed:
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	}
	snap = e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}

// wait 阻塞等待自有进程退出并分类收尾。
func (e *Engine) wait() {
	err := e.cmd.Wait()
	code := 0
	if err != nil && e.cmd.ProcessState != nil {
		code = e.cmd.ProcessState.ExitCode()
	}

	e.mu.Lock()
	e.exitCode = code
	e.stoppedAt = time.Now()
	stopped := e.stopping
	prev := e.state
	e.external = false
	if e.job != nil {
		_ = e.job.Close()
		e.job = nil
	}
	e.cmd = nil
	e.mu.Unlock()

	// 分支顺序不可换：冷启动竞速场我们的进程信使化自退（exit 0），必须先判外部接管——
	// 互斥体仍被持有说明真正存活的是外部主实例
	externalTaken := !stopped && e.probe.IsRunning()

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // 用户在 Bili23 自己退出（关窗/托盘菜单）
	default:
		e.transition(StateFailed, fmt.Sprintf(
			"Bili23 Downloader 异常退出（退出码 %d）。本应用需 Windows 10 1809+，低版本请使用上游官方 Win7 兼容版", code))
	}
}

// setStateExternal 将引擎标记为外部实例运行中（进程归属不在本引擎）。
func (e *Engine) setStateExternal() {
	var snap Snapshot
	e.mu.Lock()
	e.state = StateExternal
	e.external = true
	e.pid = 0
	e.errMsg = ""
	snap = e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}
