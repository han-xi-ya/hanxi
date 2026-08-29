// Package instance 实现 MarkerOn 单实例运行引擎：
//
// MarkerOn 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 MarkerOn
// 及其 WebView2 子进程树，杜绝孤儿标注进程。
//
// MarkerOn 无进程内 CLI 与输出协议，本引擎相应做三点适配：
//   - 身存检测仅依赖持有进程句柄的 cmd.Wait（无转手进程，无僵尸风险）；
//   - "切换标注态"依赖 MarkerOn 的单实例协议：第二次拉起 exe 会经 WM_COPYDATA 转发，
//     由主实例执行 toggle_drawing（Hidden↔Drawing）；
//   - 用户外部自行双击运行的实例（不经本引擎托管）以命名互斥体探测识别，
//     状态机单独记为 external。
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
	StateRunning  State = "running"  // 自有 Job 托管主实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 MarkerOn 主实例（非本引擎托管）
)

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"` // state==external 时为 true
	Drawing   bool      `json:"drawing"`  // 仅自有实例可信：我方 toggle 奇偶推算的标注态（起始 false=Hidden）
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本 vX.Y.Z
	Exe     string // MarkerOn.exe 绝对路径（版本隔离目录内）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("MarkerOn.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine MarkerOn 单实例运行引擎。
type Engine struct {
	mu        sync.Mutex
	state     State
	version   string
	pid       uint32
	exitCode  int
	errMsg    string
	drawing   bool // 自有实例标注态（我方 toggle 奇偶推算；用户直接按快捷键会脱同步）
	external  bool
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止标记：防止 kill 后误判为异常退出

	startMu  sync.Mutex // Start/Stop 互斥临界区
	toggleMu sync.Mutex // 信使进程拉起互斥（防抖）

	cmd    *exec.Cmd
	job    platform.Job
	jobAPI platform.JobAPI
	probe  MarkerProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe MarkerProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给 wait() 退出分类兜底。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.version = opts.Version
	e.drawing = false // 新进程初始 overlay 为 Hidden
	e.external = false
	e.stopping = false
	e.mu.Unlock()

	e.transition(StateStarting, "")

	// 刻意不设置 hideWindow 等 SysProcAttr：MarkerOn 是 GUI 子系统程序，
	// 既不产生控制台窗口，且需保留其原版行为（自带托盘，属第一阶段零 fork 承诺）
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定，保障便携标记相对路径解析
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

// Stop 停止自有实例（幂等；external 状态不在管辖范围内，直接返回 nil）。
func (e *Engine) Stop() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	if e.state != StateRunning && e.state != StateStarting {
		e.mu.Unlock()
		return nil
	}
	e.stopping = true
	cmd, job := e.cmd, e.job
	e.mu.Unlock()

	if job != nil {
		if err := job.Terminate(1); err == nil {
			return nil
		}
	}
	if cmd != nil && cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return fmt.Errorf("实例没有可终止的进程（可能仍在启动）")
}

// Toggle 拉起同路径第二个 MarkerOn 实例充当单实例协议的"信使"，
// 经 WM_COPYDATA 触发主实例 toggle_drawing。信使进程在健康主实例场 5s 内自退、
// 挂死主实例场 8s 后接管——刻意不 Wait（避免阻塞 RPC 最长 8s）、不进 Job
// （不属于托管生命周期）、Start 后立即 Release 由操作系统回收句柄。
// 此决策理由请勿在后续维护中"好心"改成 Wait，否则冷启动链路将被拖慢。
func (e *Engine) Toggle(exe string) error {
	e.toggleMu.Lock()
	defer e.toggleMu.Unlock()

	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起切换信使失败: %w", err)
	}
	_ = cmd.Process.Release()

	e.mu.Lock()
	// 仅对自有 running 实例维护奇偶推算；external/stopped 下无本地标注态可言
	if e.state == StateRunning {
		e.drawing = !e.drawing
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
	return nil
}

// RefreshExternal 探测命名互斥体校正 external/stopped 状态。
// 仅对静止态生效：running/starting 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateStopped && state != StateFailed && state != StateExternal {
		return
	}

	running := e.probe.IsMarkerOnRunning()

	e.mu.Lock()
	var snap Snapshot
	changed := false
	switch {
	case running && e.state != StateExternal:
		e.state = StateExternal
		e.external = true
		e.drawing = false
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

// WaitReady 阻塞等待 MarkerOn 主实例就绪（互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForMarkerOnReady(timeout)
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
		Drawing:   e.drawing,
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
		if s == StateStopped {
			e.drawing = false // 进程退出，标注态自然消失
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

	// 分支顺序不可换：转发场景下 exit 码同样为 0，必须先判外部接管——
	// 若退出后互斥体仍被持有，说明我们的进程是第二实例（冷启动竞速），
	// 真正存活的是外部主实例
	externalTaken := !stopped && e.probe.IsMarkerOnRunning()

	switch {
	case stopped:
		e.transition(StateStopped, "已手动停止")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "")
	default:
		e.transition(StateFailed, fmt.Sprintf("MarkerOn 异常退出（退出码 %d）。请确认已安装 WebView2 Runtime", code))
	}
}

// setStateExternal 将引擎标记为外部实例运行中（进程归属不在本引擎）。
func (e *Engine) setStateExternal() {
	var snap Snapshot
	e.mu.Lock()
	e.state = StateExternal
	e.external = true
	e.drawing = false
	e.pid = 0
	e.errMsg = ""
	snap = e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}
