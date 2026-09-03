package instance

import (
	"fmt"
	"os"
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
	StateExternal State = "external" // 外部用户自启的 Recordly 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"编辑器多窗口收起回 HUD"等非一步退出的场（上游 Windows 无托盘，
// 主窗口关闭即 app.quit，正常单窗口在宽限内完成）。
// 包级变量仅为单测可压缩等待时长，生产值保持 3s（Electron 关闭要跑
// before-quit 清理：杀采集子进程、还原光标、收导出流，比原生应用慢一档）。
var closeGracePeriod = 3 * time.Second

// externalSettle wait() 判外部接管前的静默期：进程名探测是进程树级的，
// 自有主进程刚退出时 Electron 子进程（渲染/GPU/helper）需要短暂时间随之消亡，
// 立即探测会把"自己刚退"误判成"外部实例还在"。
// 包级变量供单测压缩；生产 500ms 足够覆盖 Chromium 子进程收敛。
var externalSettle = 500 * time.Millisecond

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
	// 目录，打乱 Hanxi 版本管理；上游 updater.ts 实证官方 env 开关）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Recordly.exe 路径不能为空")
	}
	return nil
}

// mergeEnv 追加环境变量并入当前进程环境；无追加项时返回 nil
// （保持 exec 默认继承行为，与其余托管模块的零差异路径一致）。
func mergeEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine Recordly 单实例运行引擎。
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
	probe  RecordlyProbe
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe RecordlyProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：创建进程 → 绑定 JobObject → 状态 running。
// Recordly 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给 wait() 退出分类兜底
// （我方第二实例拿不到锁会 exit 0 自退，探测到进程树仍在 = 外部主实例接管）。
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

	// Recordly 是 GUI 子系统程序，无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定：与 ccswitch 同款防御性约定
	cmd.Env = mergeEnv(opts.Env)
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
//   - 已有主实例（自有/外部）→ 无参拉起"信使"进程，Electron second-instance
//     回调 restoreWindowSafely 唤起主窗口，信使自身 app.quit() 即退；
//   - 无实例 → 返回 false 由 service 层直接走 Start。
//
// 信使进程 Start 后立即 Release、刻意不 Wait（避免阻塞 RPC）、不进 Job
// （不属于托管生命周期）。此决策理由请勿在后续维护中"好心"改成 Wait
// （markeron 先例：改了就拖慢冷启动）。
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

// Quit 退出自有实例（幂等；external/stopped 状态无自有进程，直接返回 nil）：
//  1. 向 Recordly 可见窗口投递 WM_CLOSE——上游 Windows 无托盘，主窗口关闭
//     走 window-all-closed → app.quit()，before-quit 完成采集子进程清理；
//  2. 宽限 closeGracePeriod 轮询进程自然退出；
//  3. 超时（编辑器多窗口收起、未保存弹窗等）JobObject 强杀兜底——项目保存
//     上游为原子写（#741），进程级终止风险已收敛到最小。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	// 1. 尽力优雅（窗口不存在时静默 no-op）
	postClose()

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

// Exe 返回当前自有实例的可执行路径（非 running 时为空串）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// WaitReady 阻塞等待 Recordly 实例就绪（主窗口出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// IsMainWindowOpen Recordly 是否有可见窗口——空闲自动退出的豁免信号。
func (e *Engine) IsMainWindowOpen() bool {
	return e.probe.IsMainWindowOpen()
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
	// 进程树仍存活说明真正主实例是外部自启的那一个。
	// 与 ccswitch 的差异：进程名探测是树级信号，自有主进程退出后 Electron
	// 子进程需短暂收敛，先等 externalSettle 再探测，防"自己刚退"误判为外部。
	externalTaken := false
	if !stopped {
		time.Sleep(externalSettle)
		externalTaken = e.probe.IsRunning()
	}

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // 用户在 Recordly 自己退出
	default:
		e.transition(StateFailed, fmt.Sprintf(
			"Recordly 异常退出（退出码 %d）。其安装器未数字签名，若刚拉起即退出请检查杀毒软件是否拦截了未签名程序", code))
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
