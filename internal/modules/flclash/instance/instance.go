package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"hubkit/internal/platform"
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
	Version string // 绑定版本（如 6.2.0）
	// Detached 独立运行：解除 JobObject 退出联动（HubKit 关闭完全不影响工具）。
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

// Engine FlClash 单实例运行引擎。
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
	probe  FlClashProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe FlClashProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// FlClash 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示（托盘驻留）"。
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

	// FlClash 是 GUI 子系统程序，无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定：Flutter 资源按相对路径解析
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
		// 解除退出联动：HubKit 退出/崩溃不再连带杀本实例（"不随 HubKit 关闭"开关）
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

// RestoreWindow 直接唤起自有实例窗口：EnumWindows 按 PID 定位顶层窗口 →
// SW_RESTORE + SetForegroundWindow。FlClash 的第二实例有退出无唤窗
// （文件锁竞速直接 exit(0)），所以"打开窗口"不能走信使，只能直操作窗口。
func (e *Engine) RestoreWindow() {
	e.mu.Lock()
	pid := e.pid
	state := e.state
	snap := e.snapshotLocked()
	e.mu.Unlock()
	if state != StateRunning || pid == 0 {
		return
	}
	restoreWindowByPID(pid)
	e.emit(snap)
}

// RestoreExternalWindow 唤起外部实例窗口（外部 PID 未知，由探测枚举提供）。
func (e *Engine) RestoreExternalWindow(pids []uint32) {
	for _, pid := range pids {
		restoreWindowByPID(pid)
	}
}

// Quit 退出自有实例（幂等；external/stopped 状态无自有进程，直接返回 nil）：
//  1. EnumWindows 按 PID 向全部顶层窗口投递 WM_CLOSE（Flutter 关窗即退；
//     若上游改为驻托盘则窗口仅隐藏）；
//  2. 宽限 closeGracePeriod 轮询进程自然退出；
//  3. 超时（驻托盘设置/窗口未创建等）JobObject 强杀兜底。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd, pid := e.state, e.cmd, e.pid
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	// 1. 尽力优雅（窗口不存在时静默 no-op）
	postCloseByPID(pid)

	e.mu.Lock()
	e.stopping = true // 先标记：其后 wait() 无论何因收尾都归类为"手动退出"
	e.mu.Unlock()

	// 2. 宽限轮询：进程自然退出
	deadline := time.Now().Add(closeGracePeriod)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		stillRunning := e.state == StateRunning || e.state == StateStarting
		e.mu.Unlock()
		if !stillRunning {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 3. 超时强杀兜底
	return e.forceKill(cmd)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不做 WM_CLOSE 优雅退出，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待动画）。
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

// forceKill JobObject 强杀自有实例（WM_CLOSE 不可达时的兜底路径）。
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

// Exe 返回当前自有实例的可执行路径（非 running 时为空格）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// WaitReady 阻塞等待 FlClash 实例就绪（进程出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen 自有实例的可见顶层窗口是否存在——空闲自动退出的豁免信号。
func (e *Engine) IsMainWindowOpen() bool {
	e.mu.Lock()
	pid := e.pid
	state := e.state
	e.mu.Unlock()
	if state != StateRunning || pid == 0 {
		return false
	}
	return e.probe.IsMainWindowOpen([]uint32{pid})
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

// ExternalPIDs 外部实例 PID 列表（唤窗用；仅 external 语义下由 service 调用）。
func (e *Engine) ExternalPIDs() []uint32 {
	return e.probe.FindPIDs()
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

	// 分支顺序不可换：冷启动竞速场我们的进程自退（exit 0），必须先判外部接管——
	// 进程枚举仍命中说明真正存活的是外部实例
	externalTaken := !stopped && e.probe.IsRunning()

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // 用户在 FlClash 窗口自己退出
	default:
		e.transition(StateFailed, fmt.Sprintf("FlClash 异常退出（退出码 %d）。请尝试重新安装该版本", code))
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
