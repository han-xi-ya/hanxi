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
	StateExternal State = "external" // 外部用户自启的 QuickLook 主实例（非本引擎托管）
)

// closeGracePeriod 投递管道 Quit 后等待进程自然退出的宽限期，超时走强杀兜底。
// 包级变量：单测压缩、生产恢复（QuickLook OnExit 需释放互斥体/管道/键盘钩子/托盘，给足几秒）。
var closeGracePeriod = 3 * time.Second

// 管道控制消息（与上游 PipeMessages 行首字段逐字对齐）；跨平台常量，
// 实际投递由 sendPipeMessage 的 build-tag 实现决定可用性。
const (
	msgQuit   = "Quit"
	msgReload = "Reload"
)

// sendSignal 控制消息投递入口，默认绑定平台实现（sendPipeMessage）。
// 抽成包级变量供单测注入桩，避免测试误向真机上运行的 QuickLook 实例投递 Quit/Reload。
var sendSignal = sendPipeMessage

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
	Version string // 绑定版本 4.5.0
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // QuickLook.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("QuickLook.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine QuickLook 单实例运行引擎。
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
	probe  QuickLookProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe QuickLookProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// QuickLook 唯一启动语义即"无参拉起 → 驻托盘 + 全局键盘钩子生效"（预览窗口常态不可见，
// 由用户在资源管理器按空格按需触发；设置窗口须用户经托盘菜单自行打开——上游无唤窗契约）。
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

	// QuickLook 是 GUI 子系统程序，无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定版本隔离目录（portable.lock 使配置随此落地）
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

// Quit 退出引擎托管的 QuickLook（幂等；external/stopped 状态无自有进程，直接返回 nil）。
//
// 与 keyviz（无优雅通道、直接强杀）的关键差异——QuickLook 有命名管道 Quit 优雅通道：
//  1. 先投递管道 "Quit"：服务端 App.OnExit 释放互斥体/管道/键盘钩子/托盘后正常退出（exit 0）；
//  2. 宽限等待 closeGracePeriod 内进程自然收尾（wait() 归类为 stopped）；
//  3. 管道不可达/实例假死则 JobObject Terminate 强杀兜底——低级键盘钩子随进程由
//     系统自动摘除，强杀同样零残渣，故兜底安全。
//
// stopping 标记先行置位，无论走优雅退出还是强杀，wait() 收尾均归类"手动退出"（stopped）。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.stopping = true // 先标记：其后 wait() 收尾归类为"手动退出"
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	// 优雅优先：向命名管道投递 Quit，命中则等待自然退出
	if err := sendPipeMessage(msgQuit); err == nil {
		if e.waitForExit(closeGracePeriod) {
			return nil
		}
	}
	// 兜底：JobObject 强杀（进程内 LL 钩子随进程清理，零残渣）
	return e.forceKill(cmd)
}

// Reload 请求运行中的实例重载配置（管道 "Reload"，best-effort）。
// 非运行态返回错误；管道不可达返回底层错误（不影响实例存活）。
func (e *Engine) Reload() error {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateRunning {
		return fmt.Errorf("QuickLook 未在运行，无法重载配置")
	}
	return sendPipeMessage(msgReload)
}

// Stop 立即强杀自有实例（幂等）。应用退出 Shutdown 通道用：此处不做管道优雅等待，
// 保证 Hanxi 退出路径确定、不被宽限期拖慢（联动开启时由 Shutdown 调用）。
func (e *Engine) Stop() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.stopping = true
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}
	return e.forceKill(cmd)
}

// forceKill JobObject 强杀自有实例（Quit 兜底 / Stop 主路径共用的终止原语）。
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

// waitForExit 轮询引擎状态离开 running/starting（wait() 收尾即转换），超时返回 false。
// 用于 Quit 的优雅等待：不直接 cmd.Wait（已被 wait() goroutine 独占），改读状态机。
func (e *Engine) waitForExit(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		e.mu.Lock()
		st := e.state
		e.mu.Unlock()
		if st != StateRunning && st != StateStarting {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
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

// Exe 返回当前自有实例的可执行路径（非 running 时为空）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// WaitReady 阻塞等待 QuickLook 实例就绪（单实例互斥体出现），超时返回 false。
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
		e.transition(StateStopped, "") // 用户在 QuickLook 自己退出（管道 Quit / 托盘菜单等）
	default:
		e.transition(StateFailed, fmt.Sprintf("QuickLook 异常退出（退出码 %d）。请检查 Windows 运行库完整性与便携目录是否被安全软件拦截", code))
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
