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
	StateStarting State = "starting" // 外层已拉起，等待内层进程树出现（首启含解压）
	StateRunning  State = "running"  // 自有进程树（提取目录内自有 PID 闭包）存活
	StateFailed   State = "failed"   // 启动失败 / 进程树未能出现
	StateExternal State = "external" // 提取目录内有非本引擎托管的便携实例（外部自行启动）
)

// 引擎各等待窗口。包级变量仅为单测可压缩时长，生产值见注释。
var (
	// startGrace 外层拉起 → 内层进程树出现的容忍上限：
	// 首次运行需解压 ~25MB 负载，杀软实时扫描可能拖长，60s 足够宽裕。
	startGrace = 60 * time.Second
	// earlyFailWait 外层非零退出码时提前判死的观察窗（正常外层 ~1s 内退 0）
	earlyFailWait = 3 * time.Second
	// quitGrace Quit 终止进程树后等待监督协程收尾确认的宽限（Terminate 内核态秒级）
	quitGrace = 3 * time.Second
	// phase1Poll / treePoll 监督轮询间隔
	phase1Poll = 250 * time.Millisecond
	treePoll   = 400 * time.Millisecond
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
	Exe      string // rustdesk.exe（packer 外层）绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("rustdesk.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine RustDesk 托管运行引擎。
//
// 与 ccswitch/litemonitor 等先例的根本差异（勿按"常规 exe"直觉改造）：
// 被 Start 的进程是 rust-portable 外层 packer，它解压并 spawn 内层后**立即退出**
// （不 wait），因此 cmd.Wait 的返回不代表应用结束、退出码不代表应用存亡。
// 引擎把生命周期锚点换成"提取目录内自有进程树（父 PID 闭包）是否存活"，
// 由监督协程 phase1（等待出现）/ phase2（监视消失）完成状态迁移。
// JobObject 绑定外层进程，内层经继承落入同一 Job——Hanxi 退出时内核连杀整树。
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
	stopping  bool // 手动停止/退出标记：防止进程树终止后误判为异常退出

	startMu sync.Mutex // Start/Quit 互斥临界区

	cmd       *exec.Cmd
	ancestors []uint32 // 自有外层 packer PID 集合（含 SpawnWindow 派生的外层），进程树闭包锚点
	exePath   string
	job       platform.Job
	jobAPI    platform.JobAPI
	probe     RustDeskProbe
	cb        Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe RustDeskProbe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		cb:     cb,
	}
}

// Start 启动自有实例：拉起外层 → 绑定 JobObject → starting；
// 监督协程观察到内层进程树出现后升级 running。
// 本方法不做外部实例竞速探测：共享提取目录场景下自有/外部以父链闭包区分，
// 无竞态误判窗口（外层各自独立解压出各自的内层）。
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
	e.exePath = opts.Exe
	e.ancestors = nil
	e.pid = 0
	e.exitCode = 0
	e.errMsg = ""
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	e.transition(StateStarting, "")

	// GUI 子系统程序，无需隐藏控制台窗口。
	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锚定在托管目录（与先例一致；解压目标由 packer 自身决定）
	if err := cmd.Start(); err != nil {
		e.transition(StateFailed, "进程启动失败: "+err.Error())
		return err
	}

	e.mu.Lock()
	e.cmd = cmd // 立即登记
	e.ancestors = []uint32{uint32(cmd.Process.Pid)}
	e.mu.Unlock()

	job, jerr := e.jobAPI.Create()
	if jerr != nil {
		_ = cmd.Process.Kill()
		go func() { _ = cmd.Wait() }()
		e.transition(StateFailed, "创建 Job Object 失败: "+jerr.Error())
		return fmt.Errorf("创建 Job Object 失败: %w", jerr)
	}
	if aerr := job.Assign(uint32(cmd.Process.Pid)); aerr != nil {
		job.Close()
		_ = cmd.Process.Kill()
		go func() { _ = cmd.Wait() }()
		e.transition(StateFailed, "JobObject 绑定失败: "+aerr.Error())
		return fmt.Errorf("JobObject 绑定失败: %w", aerr)
	}
	if opts.Detached {
		// 解除退出联动：Hanxi 退出/崩溃不再连带杀本实例（"不随 Hanxi 关闭"开关）
		if derr := job.SetAllowKillOnClose(false); derr != nil {
			job.Close()
			_ = cmd.Process.Kill()
			go func() { _ = cmd.Wait() }()
			e.transition(StateFailed, "解除退出联动失败: "+derr.Error())
			return fmt.Errorf("解除退出联动失败: %w", derr)
		}
	}

	e.mu.Lock()
	e.job = job
	e.mu.Unlock()

	go e.supervise(cmd)
	return nil
}

// supervise 监督自有进程树（本引擎与先例 wait() 的对应物，锚点不同勿混淆）：
//
//	phase1：并行于外层回收（外层退出**不**作为状态机门控——packer 挂死也要能到期判失败），
//	        轮询"提取目录内父链命中 ancestors 的进程"，出现 → running；
//	        startGrace 内未出现（外层非零退出可提前判死）→ failed。
//	phase2：持续轮询自有树，消失 → stopped。
//
// 退出分类与模板"stopping→外部接管→exit0→failed"的对应与偏离：
//   - 外层退出码无法反映内层（packer 不 wait 内层），异常崩溃与用户自退
//     从外部不可区分——树消失统一归 stopped（stopping 标志仍优先），
//     这是本工具形态下诚实且无副作用的分类；
//   - 外部接管场景不存在（多实例合法，无单实例锁竞速自退）。
func (e *Engine) supervise(cmd *exec.Cmd) {
	codeCh := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil && cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		codeCh <- code
	}()
	exitCode := 0
	drainCode := func() {
		for {
			select {
			case c := <-codeCh:
				exitCode = c
				continue
			default:
			}
			return
		}
	}

	// ---------- phase1：等待内层进程树出现 ----------
	for {
		if e.isStopping() {
			drainCode()
			e.finish(StateStopped, "", exitCode)
			return
		}
		if own := e.probe.FindOwnPIDs(e.ancestorList()); len(own) > 0 {
			e.mu.Lock()
			e.pid = own[0]
			e.mu.Unlock()
			break
		}
		drainCode()
		since := e.sinceStart()
		if since >= startGrace || (exitCode != 0 && since >= earlyFailWait) {
			e.finish(StateFailed, fmt.Sprintf(
				"RustDesk 进程树未能出现（外层退出码 %d）。首次启动需解压内置负载，"+
					"请确认磁盘可用且未被杀软拦截/隔离后重试", exitCode), exitCode)
			return
		}
		time.Sleep(phase1Poll)
	}
	e.transition(StateRunning, "")

	// ---------- phase2：监视进程树存活 ----------
	lastPID := e.currentPID()
	for {
		own := e.probe.FindOwnPIDs(e.ancestorList())
		if len(own) == 0 {
			break
		}
		if own[0] != lastPID { // 主窗进程更替时刷新展示 PID
			lastPID = own[0]
			e.mu.Lock()
			e.pid = own[0]
			snap := e.snapshotLocked()
			e.mu.Unlock()
			e.emit(snap)
		}
		drainCode() // 外层退出码仅作诊断记录
		time.Sleep(treePoll)
	}
	drainCode()
	e.finish(StateStopped, "", exitCode)
}

// finish 收尾：释放 Job/cmd 登记并按目标状态迁移广播。
// 注意 job.Close 必须发生在确认自有树消失之后——KILL_ON_JOB_CLOSE 的语义是
// "句柄关闭时杀 Job 内进程"，提前 Close 会解除内核联动（模板先例的前提是
// cmd 即本体，本引擎的 cmd 只是短命外层）。
func (e *Engine) finish(state State, msg string, code int) {
	e.mu.Lock()
	e.exitCode = code
	if e.job != nil {
		_ = e.job.Close()
		e.job = nil
	}
	e.cmd = nil
	e.ancestors = nil
	e.mu.Unlock()
	e.transition(state, msg)
}

// RestoreWindow 唤起自有实例窗口：EnumWindows 按自有 PID 集合
// SW_RESTORE + SetForegroundWindow（等价其托盘双击语义）。
// 返回命中窗口数；0 = 驻留托盘且窗口已销毁，调用方应改用 SpawnWindow。
func (e *Engine) RestoreWindow() int {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateRunning {
		return 0
	}
	return e.probe.FocusWindows(e.probe.FindOwnPIDs(e.ancestorList()))
}

// RestoreExternalWindow 唤起外部便携实例的窗口（external 态由 service 调用）。
// 返回命中窗口数；0 = 外部实例驻留托盘无窗可唤（不越权拉起新实例）。
func (e *Engine) RestoreExternalWindow() int {
	return e.probe.FocusWindows(e.probe.FindInstancePIDs())
}

// SpawnWindow 派生第二个 packer 实例开新主窗口（自有实例驻留托盘、窗口已销毁时
// 的唯一开窗途径——上游无唤窗信使，二次拉起即新窗口，RustDesk 族允许多主窗）。
// 新外层加入同一 JobObject：其内层子树同样受退出联动与树杀管辖。
// 新进程刻意只 Wait 回收句柄、不改变状态机（树存活由 supervise 闭包轮询判定）；
// 勿"好心"把它并回主 cmd 或删掉 Wait 造成句柄泄露。
func (e *Engine) SpawnWindow() error {
	e.mu.Lock()
	job, exe, state := e.job, e.exePath, e.state
	e.mu.Unlock()
	if state != StateRunning || job == nil || exe == "" {
		return fmt.Errorf("没有可派生窗口的托管实例")
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("派生窗口实例拉起失败: %w", err)
	}
	pid := uint32(cmd.Process.Pid)
	if aerr := job.Assign(pid); aerr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("派生实例绑定 JobObject 失败: %w", aerr)
	}
	e.mu.Lock()
	e.ancestors = append(e.ancestors, pid)
	e.mu.Unlock()
	go func() { _ = cmd.Wait() }()
	return nil
}

// Quit 退出自有实例（幂等；external/stopped 状态无自有进程，直接返回 nil）。
// 上游无优雅退出通道（WM_CLOSE 只会再藏窗、无 --quit CLI），
// 直接 JobObject 终止整树——进行中的远程会话会被切断（托管端停止服务的预期语义）。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state, cmd := e.state, e.cmd
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	e.mu.Lock()
	e.stopping = true // 先标记：supervise 无论何因收尾都归类为"手动退出"
	e.mu.Unlock()

	kerr := e.forceKill(cmd)

	// 宽限等待监督协程观察到树消失并完成状态收尾（Terminate 通常亚秒生效）
	deadline := time.Now().Add(quitGrace)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		stillActive := e.state == StateRunning || e.state == StateStarting
		e.mu.Unlock()
		if !stillActive {
			return kerr
		}
		time.Sleep(100 * time.Millisecond)
	}
	return kerr
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不等收尾（应用退出时
// Shutdown 通道用，Job 联动 + 进程退出本身都会兜底）。
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

// forceKill JobObject 终止自有实例整树（cmd 此刻多已退出，Kill 仅兜底未绑定 Job 的窗口）。
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

// RefreshExternal 探测外部便携实例校正 external/stopped 状态。
// 仅对静止态生效：running/starting 时提取目录内的进程本就有自有部分，会误导状态机。
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

// Exe 返回当前托管实例的可执行路径（未启动过时为空）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exePath
}

// WaitReady 阻塞等待自有进程树出现（状态升为 running），超时/失败返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		e.mu.Lock()
		state := e.state
		e.mu.Unlock()
		switch state {
		case StateRunning:
			return true
		case StateFailed, StateStopped, StateExternal:
			return false
		}
		if time.Now().After(deadline) {
			return e.Snapshot().State == StateRunning
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// IsMainWindowOpen 自有实例是否存在可见顶层窗口（横幅提示"驻留托盘可再开窗"用）。
func (e *Engine) IsMainWindowOpen() bool {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateRunning {
		return false
	}
	return e.probe.HasVisibleWindow(e.probe.FindOwnPIDs(e.ancestorList()))
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

// ---------- 内部小工具（锁纪律：快照与广播均在锁外） ----------

func (e *Engine) isStopping() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopping
}

func (e *Engine) ancestorList() []uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]uint32(nil), e.ancestors...)
}

func (e *Engine) currentPID() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pid
}

func (e *Engine) sinceStart() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startedAt.IsZero() {
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
