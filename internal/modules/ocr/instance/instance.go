package instance

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/ringbuf"
)

// logCapacity 内存环形日志容量（行）。hanxi-ocr 行频极低，仅作排障取证。
const logCapacity = 1000

// LogCapacityHint 对外暴露环形缓冲容量，供 service 层截断"取最近 N 行"上界。
const LogCapacityHint = logCapacity

// readyTimeout Start 后等待服务就绪（端口连通 + 契约应答）的上限。
// 上游冷启动仅百毫秒级，15s 已含杀毒扫描首启等极端情况。
var readyTimeout = 15 * time.Second

// quitSettleWait Stop 终止进程后等待 wait() 协程收尾状态的上限。
var quitSettleWait = 3 * time.Second

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（含就绪等待窗口）
	StateRunning  State = "running"  // 自有 Job 托管实例运行中（服务契约已就绪）
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部自行启动的 hanxi-ocr（非本引擎托管，只转发不管理）
)

// Snapshot 引擎状态快照：事件推送与 service 层状态合并共用同一模型。
type Snapshot struct {
	State      State     `json:"state"`
	PID        uint32    `json:"pid"`
	ExitCode   int       `json:"exitCode"`
	Error      string    `json:"error"`
	External   bool      `json:"external"`   // state==external 时为 true
	ListenAddr string    `json:"listenAddr"` // 自有实例监听地址（external/stopped 可能为空）
	StartedAt  time.Time `json:"startedAt"`
	StoppedAt  time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析设置项后填充。
type StartOptions struct {
	// Exe hanxi-ocr.exe 绝对路径。
	Exe string
	// ListenAddr 服务监听地址，恒为 127.0.0.1:port（上游本身就只绑回环）。
	ListenAddr string
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭不影响服务）。
	Detached bool
}

func (o StartOptions) validate() error {
	switch {
	case o.Exe == "":
		return fmt.Errorf("hanxi-ocr.exe 路径不能为空")
	case o.ListenAddr == "":
		return fmt.Errorf("监听地址不能为空")
	}
	return nil
}

// buildServeArgs 上游启动参数协议（v0.2.0 源码实证）：serve 子命令 +
// -port 指定端口；裸跑等价于默认 53120。
func buildServeArgs(listenAddr string) []string {
	port := listenAddr
	if i := strings.LastIndex(listenAddr, ":"); i >= 0 {
		port = listenAddr[i+1:]
	}
	return []string{"serve", "-port", port}
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
	OnLog   func(line string)
}

// Engine hanxi-ocr 单实例运行引擎。
type Engine struct {
	mu        sync.Mutex
	state     State
	pid       uint32
	exitCode  int
	errMsg    string
	external  bool
	listen    string
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止标记：防止进程终止后误判为异常退出

	startMu sync.Mutex // Start/Stop 互斥临界区

	cmd    *exec.Cmd
	job    platform.Job
	jobAPI platform.JobAPI
	probe  Probe
	logs   *ringbuf.RingBuffer
	cb     Callbacks
}

func NewEngine(jobAPI platform.JobAPI, probe Probe, cb Callbacks) *Engine {
	return &Engine{
		state:  StateStopped,
		jobAPI: jobAPI,
		probe:  probe,
		logs:   ringbuf.New(logCapacity),
		cb:     cb,
	}
}

// Start 启动自有实例并同步等待服务就绪：
// 端口预检（契约判别 external / 占用判 failed）→ 隐藏控制台拉起
// → JobObject 绑定 → "端口连通 + 契约应答"双条件轮询。
// 就绪失败即终止自有进程并落 failed——端口活着但契约不通（被其他程序抢绑）
// 不能算启动成功。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.listen = opts.ListenAddr
	e.external = false
	e.stopping = false
	e.mu.Unlock()

	// 端口预检：已被占用时以契约探测甄别是否外部 hanxi-ocr 实例。
	if e.probe.PortOpen(opts.ListenAddr) {
		if e.probe.IsOCRService(opts.ListenAddr) {
			e.setStateExternal()
			return fmt.Errorf("检测到外部 hanxi-ocr 已在 %s 服务（非 Hanxi 托管），识别照常、启停不接管", opts.ListenAddr)
		}
		e.transition(StateFailed, fmt.Sprintf("端口 %s 已被其他程序占用，可在设置中更换服务端口", opts.ListenAddr))
		return fmt.Errorf("端口 %s 已被占用", opts.ListenAddr)
	}

	e.transition(StateStarting, "")

	cmd := exec.Command(opts.Exe, buildServeArgs(opts.ListenAddr)...)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定组件目录（引擎资产按 exe 同级定位）
	hideWindow(cmd)                  // 控制台子系统程序，不抑制会闪黑窗
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.transition(StateFailed, "打开进程输出管道失败: "+err.Error())
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		e.transition(StateFailed, "打开进程错误管道失败: "+err.Error())
		return err
	}
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

	go e.pump(stdout)
	go e.pump(stderr)
	go e.wait()

	if !e.waitReady(opts.ListenAddr, readyTimeout) {
		// 就绪失败：端口可能被抢绑或引擎文件损坏导致契约永不应答，主动终止收尾。
		e.mu.Lock()
		e.stopping = true
		job := e.job
		cmdRef := e.cmd
		e.mu.Unlock()
		if job != nil {
			_ = job.Terminate(1)
		} else if cmdRef != nil && cmdRef.Process != nil {
			_ = cmdRef.Process.Kill()
		}
		e.settleExited(quitSettleWait)
		msg := fmt.Sprintf("启动后 %s 服务始终不可用（端口被抢绑或引擎文件不完整），已终止", opts.ListenAddr)
		e.transition(StateFailed, msg)
		return fmt.Errorf("%s", msg)
	}

	e.transition(StateRunning, "")
	return nil
}

// waitReady 轮询"端口连通 + 契约应答"双条件直至就绪；进程中途退出则末位复探后放弃。
func (e *Engine) waitReady(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if e.probe.PortOpen(addr) && e.probe.IsOCRService(addr) {
			return true
		}
		e.mu.Lock()
		dead := e.cmd == nil
		e.mu.Unlock()
		if dead {
			// 末位复探：进程退出与就绪可能同帧发生
			return e.probe.PortOpen(addr) && e.probe.IsOCRService(addr)
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

// settleExited 等待 wait() 协程完成收尾（cmd 注销）直至上限。
func (e *Engine) settleExited(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		done := e.cmd == nil
		e.mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop 终止自有实例（幂等；external/stopped 无自有进程直接返回 nil）。
// 优雅退出（POST /api/shutdown）由 service 层在调用本方法前尝试，本引擎只留强杀兜底。
func (e *Engine) Stop() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	e.mu.Lock()
	e.stopping = true // 先标记：其后 wait() 无论何因收尾都归类为"手动停止"
	e.mu.Unlock()
	if err := e.forceKill(); err != nil {
		return err
	}
	e.settleExited(quitSettleWait)
	return nil
}

// forceKill JobObject 终止自有实例。
func (e *Engine) forceKill() error {
	e.mu.Lock()
	job := e.job
	cmd := e.cmd
	e.mu.Unlock()
	if job != nil {
		return job.Terminate(1)
	}
	if cmd != nil && cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return fmt.Errorf("实例没有可终止的进程")
}

// RefreshExternal 探测校正 external/stopped 状态（仅对静止态生效：
// running/starting 时探测到的正是自己，会误导状态机）。
// listenAddr 为当前设定端口；空串时跳过契约探测（无从探起）。
func (e *Engine) RefreshExternal(listenAddr string) {
	if listenAddr == "" {
		return // 无探测目标不做判断（保守：不把 external 误归 stopped）
	}
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateStopped && state != StateFailed && state != StateExternal {
		return
	}

	var serving bool
	if e.probe.PortOpen(listenAddr) {
		serving = e.probe.IsOCRService(listenAddr)
	}

	e.mu.Lock()
	var snap Snapshot
	changed := false
	switch {
	case serving && e.state != StateExternal:
		e.state = StateExternal
		e.external = true
		e.listen = listenAddr
		e.pid = 0
		e.errMsg = ""
		changed = true
	case !serving && e.state == StateExternal:
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

// Exe 返回当前自有实例的可执行路径（非自有实例时为空串）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd == nil {
		return ""
	}
	return e.cmd.Path
}

// PortOpen 代理探测指定地址 TCP 连通性。
func (e *Engine) PortOpen(addr string) bool { return e.probe.PortOpen(addr) }

// Logs 返回最近 n 行进程输出。
func (e *Engine) Logs(n int) []string { return e.logs.Last(n) }

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
		State:      e.state,
		PID:        e.pid,
		ExitCode:   e.exitCode,
		Error:      e.errMsg,
		External:   e.external,
		ListenAddr: e.listen,
		StartedAt:  e.startedAt,
		StoppedAt:  e.stoppedAt,
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
	listen := e.listen
	e.external = false
	if e.job != nil {
		_ = e.job.Close()
		e.job = nil
	}
	e.cmd = nil
	e.mu.Unlock()

	// 分支顺序不可换：stopping 优先保证手动退出归为 stopped；
	// 其后外部接管判定——我们的进程没了但端口上仍有 hanxi-ocr 契约应答
	// （用户自启实例并存场景）必须落 external 而非误报失败。
	externalTaken := !stopped && listen != "" && e.probe.IsOCRService(listen)

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // 服务自行退出（如闲置 -exit-on-idle 模式）
	default:
		e.transition(StateFailed, fmt.Sprintf(
			"hanxi-ocr 异常退出（退出码 %d）。常见原因：端口 %s 启动后被抢绑、引擎文件被杀毒软件删除，详见服务目录日志",
			code, e.listen))
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

// pump 逐行搬运子进程输出：环形缓冲 → 事件回调。
func (e *Engine) pump(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			e.writeLog(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			return
		}
	}
}

// writeLog 统一入口：环形缓冲 → 回调（hanxi-ocr 输出不含敏感信息，无脱敏需求）。
func (e *Engine) writeLog(line string) {
	e.logs.Write(line)
	if e.cb.OnLog != nil {
		e.cb.OnLog(line)
	}
}
