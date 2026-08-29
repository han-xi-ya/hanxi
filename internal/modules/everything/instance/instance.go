// Package instance 实现 Everything 单实例运行引擎：
//
// Everything 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出，内核都会连带终止 Everything 进程树，杜绝孤儿驻留。
//
// 与 markeron 引擎的两点本质差异：
//   - Everything 无"标注开关"语义，本引擎提供三操作：后台启动（-startup）、
//     唤起窗口（无参二次启动，单实例协议自动转发给主实例）、优雅退出（-quit 经 IPC
//     转发给主实例，先落盘索引库再退出；超时则 JobObject 强杀兜底）；
//   - 运行模式（后台驻留/窗口）由启动参数决定，仅作信息提示，不承诺精确——
//     用户随时可以关闭搜索窗口而不退出实例，模式即回到后台。
//
// 本包零框架依赖，便于单元测试。
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
	StateRunning  State = "running"  // 自有 Job 托管实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 Everything 实例（非本引擎托管）
)

// Mode 运行模式（信息提示用）
const (
	ModeBackground = "background" // -startup：后台驻留 + 托盘，不显示搜索窗
	ModeWindow     = "window"     // 无参启动：直接显示搜索窗
)

// quitGracePeriod Everything -quit 优雅退出的等待窗口：超时则 JobObject 强杀兜底。
// 包级变量仅为单测可压缩等待时长，生产值保持 5s。
var quitGracePeriod = 5 * time.Second

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
	Exe     string // Everything.exe 绝对路径（版本隔离目录内）
	Mode    string // ModeBackground / ModeWindow
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

// Engine Everything 单实例运行引擎。
type Engine struct {
	mu        sync.Mutex
	state     State
	version   string
	pid       uint32
	exitCode  int
	errMsg    string
	mode      string
	external  bool
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止/退出标记：防止进程终止后误判为异常退出

	startMu sync.Mutex // Start/Stop/Quit 互斥临界区

	cmd    *exec.Cmd
	job    platform.Job
	jobAPI platform.JobAPI
	probe  EverythingProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe EverythingProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// background 模式追加 -startup（后台驻留不显示窗口）；window 模式无参直接起窗。
// 本方法不做存在性探测：冷启动与外部实例竞速的 TOCTOU 交给 wait() 退出分类兜底。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.version = opts.Version
	e.mode = opts.Mode
	e.external = false
	e.stopping = false
	e.mu.Unlock()

	e.transition(StateStarting, "")

	// 内嵌托管：隐藏 Everything 托盘图标——实例启停/唤窗全由 Hanxi 按钮接管。
	// 失败静默容忍：托盘可见不影响功能，仅少一层"纯内嵌"观感。
	_ = ensureHiddenTray(filepath.Join(filepath.Dir(opts.Exe), "Everything.ini"))

	// Everything 是 GUI 子系统程序，无需隐藏控制台窗口；
	// 其余行为刻意保留原版（索引、全局热键不受托盘隐藏影响）
	var args []string
	if opts.Mode == ModeBackground {
		args = []string{"-startup"}
	}
	cmd := exec.Command(opts.Exe, args...)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定：便携版配置与索引库落在 exe 同目录
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

	e.mu.Lock()
	e.job = job
	e.mu.Unlock()

	go e.wait()
	e.transition(StateRunning, "")
	return nil
}

// OpenWindow 完成单例协议唤起搜索窗口：
//   - 已有主实例（自有/外部）→ 无参拉起"信使"进程，单实例协议自动转发给主实例显示窗口；
//   - 无实例 → 返回 false 由 service 层走窗口模式 Start。
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
	own := e.state == StateRunning
	if own {
		// 自有实例：窗口自此可见（用户随后关闭窗口也会脱同步——Mode 仅信息提示）
		e.mode = ModeWindow
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
	return true, nil
}

// Quit 优雅退出自有实例：经 -quit 信使请求主实例落盘退出。
// 宽限 quitGracePeriod 内进程未退出则 JobObject 强杀兜底（索引库状态由 Everything 定期落盘保障）。
// 幂等；external/stopped 状态无自有进程，直接返回 nil。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, exe, cmd := e.state, e.exeLocked(), e.cmd
	e.mu.Unlock()

	if state != StateRunning && state != StateStarting {
		return nil
	}
	if exe == "" {
		// 进程刚创建尚未登记 exe：直接强杀兜底
		return e.forceKill(cmd)
	}

	// 1. 优雅退出信使：-quit 无需等待（转发后立即退出）
	quitCmd := exec.Command(exe, "-quit")
	quitCmd.Dir = filepath.Dir(exe)
	if err := quitCmd.Start(); err != nil {
		return e.forceKill(cmd)
	}
	_ = quitCmd.Process.Release()

	e.mu.Lock()
	e.stopping = true // 先标记：其后 wait() 无论何因收尾都归类为"手动退出"
	e.mu.Unlock()

	// 2. 宽限轮询：进程自然退出
	deadline := time.Now().Add(quitGracePeriod)
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

// forceKill JobObject 强杀自有实例（-quit 不可达时的兜底路径）。
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

// Stop 立即强杀自有实例（幂等；external 状态不在管辖范围内，直接返回 nil）。
// 与 Quit 的差别：不做 -quit 优雅退出，直接 JobObject 终止。
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

// RefreshExternal 探测外部实例校正 external/stopped 状态。
// 仅对静止态生效：running/starting 时探测到的正是自己（或另一并存的通道实例），会误导状态机。
func (e *Engine) RefreshExternal() {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateStopped && state != StateFailed && state != StateExternal {
		return
	}

	running := e.probe.IsEverythingRunning()

	e.mu.Lock()
	var snap Snapshot
	changed := false
	switch {
	case running && e.state != StateExternal:
		e.state = StateExternal
		e.external = true
		e.mode = "" // 外部实例的启停形态不可知，不揣测（UI 以"外部运行"呈现）
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
	return e.exeLocked()
}

func (e *Engine) exeLocked() string {
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// WaitReady 阻塞等待 Everything 实例就绪（托盘通知窗口出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForEverythingReady(timeout)
}

// IsSearchWindowOpen 搜索主窗口是否打开——空闲自动退出的豁免信号
// （窗口开着说明用户可能正在其中输入，不能静默退出）。
func (e *Engine) IsSearchWindowOpen() bool {
	return e.probe.IsSearchWindowOpen()
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
		Mode:      e.mode,
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

	// 分支顺序不可换：优雅退出（-quit）的 exit 码同样为 0，必须先判外部接管——
	// 若退出后实例探测仍命中，说明隔壁还存活着另一实例（1.4/1.5 通道并存场景），
	// 我们的进程退出后真正在跑的是它
	externalTaken := !stopped && e.probe.IsEverythingRunning()

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // 用户在 Everything 托盘自己退出了
	default:
		e.transition(StateFailed, fmt.Sprintf("Everything 异常退出（退出码 %d）。请检查索引库是否损坏", code))
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
