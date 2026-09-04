package instance

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/ringbuf"
)

// logCapacity 内存环形日志容量（行）。
const logCapacity = 1000

// LogCapacityHint 对外暴露环形缓冲容量，供 service 层截断"取最近 N 行"上界。
const LogCapacityHint = logCapacity

const (
	// daemonEnvKey/daemonEnvVal 上游 main.go 实证的后门：DDNS_GO_DAEMON=1
	// 跳过 kardianos/service 状态检测直跑 run()。用户若曾用 `-s install`
	// 装过同名 Windows 服务，裸启动 exe 会被劫持进 SCM 路径立即失败退出，
	// 注入该变量后此雷区对托管启动免疫。
	daemonEnvKey = "DDNS_GO_DAEMON"
	daemonEnvVal = "1"

	// configFileName 上游配置恒存位置：%USERPROFILE%\.ddns_go_config.yaml
	// （util/user.go GetConfigFilePathDefault）。
	configFileName = ".ddns_go_config.yaml"
)

// readyTimeout Start 后等待 web 端口就绪的上限。端口绑定失败的进程
// 会存活一分钟才退出（上游特性），就绪等待不能以"进程还活着"作成功信号。
var readyTimeout = 20 * time.Second

// quitSettleWait Quit 终止进程后等待 wait() 协程收尾状态的上限。
var quitSettleWait = 3 * time.Second

// configSettleQuiet / configSettleMax 配置写静默期防护参数（Quit 前生效）：
// 上游 SaveConfig 为裸 os.WriteFile（非原子 tmp+rename），强杀恰好撞上
// webui 保存会截毁用户配置文件。mtime 距今小于 Quiet 视为"正在保存"，
// 推迟终止直至静默或到达 Max 上限。包级变量便于单测压缩时长。
var (
	configSettleQuiet = 1500 * time.Millisecond
	configSettleMax   = 5 * time.Second
)

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（含 web 就绪等待窗口）
	StateRunning  State = "running"  // 自有 Job 托管实例运行中（web 端口已就绪）
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部自行启动/服务形态的 ddns-go（非本引擎托管）
)

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Version    string    `json:"version"`
	State      State     `json:"state"`
	PID        uint32    `json:"pid"`
	ExitCode   int       `json:"exitCode"`
	Error      string    `json:"error"`
	External   bool      `json:"external"`   // state==external 时为 true
	ListenAddr string    `json:"listenAddr"` // 自有实例监听地址（external/stopped 可能为空）
	StartedAt  time.Time `json:"startedAt"`
	StoppedAt  time.Time `json:"stoppedAt"`
}

// LogEntry 单条实例日志行（事件 ddnsgo:instance-log 载荷）。
type LogEntry struct {
	Line string `json:"line"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本 vX.Y.Z
	Exe     string // ddns-go.exe 绝对路径（版本隔离目录内）
	// ListenAddr web 服务监听地址，恒为 127.0.0.1:port（回环绑定，
	// 面板不外露局域网）。
	ListenAddr string
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
}

func (o StartOptions) validate() error {
	switch {
	case o.Exe == "":
		return fmt.Errorf("ddns-go.exe 路径不能为空")
	case o.ListenAddr == "":
		return fmt.Errorf("监听地址不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
	OnLog   func(entry LogEntry)
}

// Engine ddns-go 单实例运行引擎。
type Engine struct {
	mu        sync.Mutex
	state     State
	version   string
	pid       uint32
	exitCode  int
	errMsg    string
	external  bool
	listen    string
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止/退出标记：防止进程终止后误判为异常退出

	startMu sync.Mutex // Start/Quit 互斥临界区

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

// Start 启动自有实例并同步等待 web 端口就绪：
// 端口预检 → 隐藏控制台拉起（注入 DDNS_GO_DAEMON=1 绕开上游服务劫持路径）
// → JobObject 绑定 → TCP 就绪轮询。
// 就绪失败即终止自有进程并落 failed——上游端口冲突时进程会"僵活"一分钟，
// 不能以进程存活为成功信号。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.version = opts.Version
	e.listen = opts.ListenAddr
	e.external = false
	e.stopping = false
	e.mu.Unlock()

	// 端口预检：已被占用时先甄别是否外部 ddns-go（进程名扫描），
	// 避免冷启动竞速误伤（TOCTOU 残余窗口交给就绪轮询兜底）。
	if e.probe.PortOpen(opts.ListenAddr) {
		if e.probe.IsRunning() {
			e.setStateExternal()
			return fmt.Errorf("检测到 ddns-go 已在 %s 运行（非 Hanxi 托管），请先在其窗口/服务中退出", opts.ListenAddr)
		}
		e.transition(StateFailed, fmt.Sprintf("端口 %s 已被其他程序占用，可在设置中更换监听端口", opts.ListenAddr))
		return fmt.Errorf("端口 %s 已被占用", opts.ListenAddr)
	}

	e.transition(StateStarting, "")

	cmd := exec.Command(opts.Exe, "-l", opts.ListenAddr)
	cmd.Dir = filepath.Dir(opts.Exe) // 工作目录锁定到版本隔离目录
	// 上游源码实证：DDNS_GO_DAEMON=1 跳过 kardianos/service 检测直跑 run()。
	// 用户机器上若残留同名 Windows 服务安装，此注入是托管启动不被劫持的唯一防线。
	cmd.Env = append(os.Environ(), daemonEnvKey+"="+daemonEnvVal)
	hideWindow(cmd) // ddns-go 是控制台子系统程序，不抑制会闪黑窗
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

	if !e.waitPortReady(opts.ListenAddr, readyTimeout) {
		// 就绪失败：进程可能僵活（上游端口冲突不即退特性），主动终止收尾。
		// stopping 置位让 wait() 归为"手动停止"，具体原因以本处覆盖文案为准。
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
		msg := fmt.Sprintf("启动后端口 %s 始终不可用（可能有其他进程抢绑，或上游检测到同名服务环境异常），已终止", opts.ListenAddr)
		e.transition(StateFailed, msg)
		return fmt.Errorf("%s", msg)
	}

	e.transition(StateRunning, "")
	return nil
}

// waitPortReady 轮询 TCP 连通直至 web 服务就绪；进程中途退出则提前放弃。
func (e *Engine) waitPortReady(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if e.probe.PortOpen(addr) {
			return true
		}
		e.mu.Lock()
		dead := e.cmd == nil
		e.mu.Unlock()
		if dead {
			// 末位复探：进程退出与端口就绪可能同帧发生
			return e.probe.PortOpen(addr)
		}
		time.Sleep(150 * time.Millisecond)
	}
	return e.probe.PortOpen(addr)
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

// Quit 退出自有实例（幂等；external/stopped 状态无自有进程，直接返回 nil）。
// 上游无优雅退出通道（无信号处理、无管理 API），退出即进程级终止；
// 唯一需要保护的是配置写窗口——SaveConfig 非原子，终止前先过配置写静默期。
func (e *Engine) Quit() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state != StateRunning && state != StateStarting {
		return nil
	}

	e.mu.Lock()
	e.stopping = true // 先标记：其后 wait() 无论何因收尾都归类为"手动退出"
	e.mu.Unlock()

	quiesceConfigWrite()

	if err := e.forceKill(); err != nil {
		return err
	}
	e.settleExited(quitSettleWait)
	return nil
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：跳过配置写静默期等待——
// 应用退出通道（OnShutdown）必须限时返回，不能让用户主程序等待。
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
	e.stopping = true
	e.mu.Unlock()
	if err := e.forceKill(); err != nil {
		return err
	}
	e.settleExited(quitSettleWait)
	return nil
}

// quiesceConfigWrite 配置写静默期：若 %USERPROFILE%\.ddns_go_config.yaml 的
// mtime 距今不足 configSettleQuiet，等待其静默（上限 configSettleMax）再返回，
// 避免 JobObject 强杀恰好截断上游的裸 os.WriteFile。配置文件不存在
// （首次使用用户）或 stat 异常时直接返回（无写风险）。
func quiesceConfigWrite() {
	path := upstreamConfigPath()
	if path == "" {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	deadline := time.Now().Add(configSettleMax)
	for configQuiescencePending(fi.ModTime(), time.Now()) {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(200 * time.Millisecond)
		fi, err = os.Stat(path)
		if err != nil {
			return
		}
	}
}

// configQuiescencePending 纯函数判定：now 时刻该 mtime 是否仍处于写静默观察期。
// mtime 在未来（NTP 回拨/时钟异常文件系统）不等待——否则白耗满静默上限。
func configQuiescencePending(mtime, now time.Time) bool {
	if mtime.After(now) {
		return false
	}
	return now.Sub(mtime) < configSettleQuiet
}

// upstreamConfigPath 返回上游约定配置路径；家目录不可得时返回空串。
func upstreamConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, configFileName)
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

// ListenAddr 返回自有实例监听地址（未运行返回空串）。
func (e *Engine) ListenAddr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateRunning && e.state != StateStarting {
		return ""
	}
	return e.listen
}

// PortOpen 代理探测指定地址是否已有 ddns-go web 服务（external 场景定位面板）。
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
		Version:    e.version,
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
	e.external = false
	if e.job != nil {
		_ = e.job.Close()
		e.job = nil
	}
	e.cmd = nil
	e.mu.Unlock()

	// 分支顺序不可换：stopping 优先保证手动退出归为 stopped；
	// 其后外部接管判定——我们的进程没了但 ddns-go.exe 仍在
	// （用户自启实例/服务实例并存场景）必须落 external 而非误报失败
	externalTaken := !stopped && e.probe.IsRunning()

	switch {
	case stopped:
		e.transition(StateStopped, "")
	case externalTaken && prev != StateFailed:
		e.setStateExternal()
	case code == 0 && prev == StateRunning:
		e.transition(StateStopped, "") // ddns-go 自身退出（如上游 -u 自升级路径，托管下不主动使用）
	default:
		e.transition(StateFailed, fmt.Sprintf(
			"ddns-go 异常退出（退出码 %d）。常见原因：端口 %s 启动后被抢绑、配置文件不可写，详见日志",
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

// pump 逐行搬运子进程输出：脱敏 → 环形缓冲 → 事件回调。
func (e *Engine) pump(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			e.writeLog(line)
		}
		if err != nil {
			return
		}
	}
}

// writeLog 统一入口：脱敏 → 环形缓冲 → 回调。
func (e *Engine) writeLog(line string) {
	line = scrubSecrets(line)
	e.logs.Write(line)
	if e.cb.OnLog != nil {
		e.cb.OnLog(LogEntry{Line: line})
	}
}

// secretQueryRe ddns-go 的 DNS 服务商凭据多以 URL query / 表单字段形态参与请求，
// 异常栈与调试日志可能整串带出。通用脱敏：常见敏感参数名的取值置换为 ***。
// （上游 web 日志页有自带脱敏，但 stdout 通道没有，托管侧必须自行设防。）
var secretQueryRe = regexp.MustCompile(`(?i)((?:token|secret|password|passwd|access[_-]?key(?:[_-]?id|[_-]?secret)?|accesskey|apikey|api_key|ak|sig|signature)=)[^&\s"']+`)

func scrubSecrets(line string) string {
	return secretQueryRe.ReplaceAllString(line, "${1}***")
}
