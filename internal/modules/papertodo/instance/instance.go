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
	StateExternal State = "external" // 外部用户自启的 PaperTodo 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的"exit 命令信使"优雅退出宽限：主实例收到命令后自行
// 保存数据并退出；超时（管道未建好/主实例卡死）则 JobObject 强杀兜底。
// 包级变量仅为单测可压缩等待时长，生产值保持 3s（便签含图片库落盘稍重）。
var closeGracePeriod = 3 * time.Second

// launchMessenger 拉起"信使"二次实例：PaperTodo 的 SingleInstanceHelper 会把
// 其命令行参数经命名管道转发给主实例（show/hide/exit 命令词表见包注释），
// 随后信使自行退出。Start 后立即 Release、刻意不 Wait（避免阻塞 RPC）、
// 不进 Job（不属于托管生命周期）。此决策请勿在后续维护中"好心"改成 Wait。
// 包级变量便于单测注入（真实信使是 GUI 进程，测试只断言命令与参数）。
var launchMessenger = func(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

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

// Engine PaperTodo 单实例运行引擎。
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
	probe  PaperProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe PaperProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// PaperTodo 无后台启动 CLI，启动语义即"无参拉起 → 纸片出现在桌面 + 托盘常驻"。
// 若外部主实例已在场，本次进程会自动信使化并退出，由 wait() 外部接管分类兜底。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给 wait() 退出分类处理。
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

	// PaperTodo 是 GUI 子系统程序，无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定：便签数据按 exe 同目录相对路径读写
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

// OpenWindow 唤回纸片：向主实例（自有或外部）发送 show 命令信使——
// 上游回调 ShowAllPapers()，把散落/折叠的纸片全部找回。
// 信使语义约束见 launchMessenger 注释（勿改成 Wait）。
func (e *Engine) OpenWindow(exe string) error {
	if err := launchMessenger(exe, "show"); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	e.mu.Lock()
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
	return nil
}

// HidePapers 收拢纸片：hide 命令信使（主实例在场才有意义，service 层限定状态）。
func (e *Engine) HidePapers(exe string) error {
	if err := launchMessenger(exe, "hide"); err != nil {
		return fmt.Errorf("拉起收拢信使失败: %w", err)
	}
	return nil
}

// Quit 退出引擎托管的 PaperTodo（幂等；external/stopped 状态无自有进程，直接返回 nil）：
//  1. 发送 exit 命令信使——上游官方优雅通道（保存数据后自退），
//     比 markeron 时代的裸强杀多一层体面；管道监听未就绪时信使会静默失败；
//  2. 宽限 closeGracePeriod 轮询进程自然退出；
//  3. 超时 JobObject 强杀兜底——上游"写盘前自动快照备份"，强杀最坏丢当前
//     未保存输入且有快照可回，非常态路径。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	// 1. 尽力优雅：exit 命令信使（自有实例 exe 即管道服务端同机进程）。
	// 信使拉不起不阻断退出流程（管道监听未就绪等场）：宽限后强杀兜底。
	if cmd != nil && cmd.Path != "" {
		_ = launchMessenger(cmd.Path, "exit")
	}

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

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不派 exit 信使、不等宽限，
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

// forceKill JobObject 强杀自有实例（exit 信使不可达时的兜底路径）。
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

// WaitReady 阻塞等待 PaperTodo 实例就绪（单实例互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
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
		e.transition(StateStopped, "") // 用户在 PaperTodo 托盘菜单自行退出
	default:
		e.transition(StateFailed, fmt.Sprintf("PaperTodo 异常退出（退出码 %d）。若使用 no-runtime 精简变体，请确认系统已安装 .NET 10 桌面运行时（环境检测页可查看）", code))
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
