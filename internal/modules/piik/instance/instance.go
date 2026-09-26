package instance

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/ringbuf"
)

// DefaultPort 上游默认监听端口（8787 起、被占 +1 扫描 ≤10 的分配纪律归
// service 层执行，instance 只收定值——Options.Port 即裁决后的定值）。
const DefaultPort = 8787

// logCapacity 内存环形日志容量（行，frpc 同值）。
const logCapacity = 1000

// LogCapacityHint 对外暴露环形缓冲容量，供 service 层截断"取最近 N 行"上界。
const LogCapacityHint = logCapacity

// 时长参数包级变量：单测压缩等待窗口（内核/蓝本同款手法），生产值见注释。
var (
	// quitGrace Quit 优雅窗口预算：上游实证 stdin 收到任意字节后内部 5s
	// 预算优雅停机——宽限须覆盖该预算并留余量，取 6s；超期 JobObject
	// Terminate 兜底（整树含 cloudflared/piik-capture 孙进程）。
	quitGrace = 6 * time.Second
	// settleTimeout 强制终止后等待 wait 协程收口（终态回写完成）的上限
	//（frpc Stop 同值 10s）。
	settleTimeout = 10 * time.Second
)

// errBusy 上一个受管进程尚未收口（wait 未完成），拒绝新启动（内核
// supervisor.ErrBusy 同纪律：不隐式重启、不并发双 Wait）。
var errBusy = errors.New("instance: 上一个 piik 进程尚未收口，请稍后重试")

// State 引擎状态机（对前端五态）：stopped → starting → running →
// (stopped | failed | external)。优雅停收口相（stdin 信令已投、宽限/兜底
// 终止进行中）在引擎内部以 run.stopping 标记承载，**展示层折并入 running**
// ——ccswitch/dbx/paseo 家族 mapState 同款口径（终态由收口后的 stopped/
// failed 广播纠正；B/C 线契约注释已锁"无 quitting 档"）。
// StateQuitting 为预留词表位：当前实现从不广播该值，联编面若有人引用
// 仍可编译（永不到来的档——前端删档或本包补档的裁决归联编收口清单）。
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建与 Job 绑定窗口（极短）
	StateRunning  State = "running"  // 自有 Job 托管实例运行中（含收口窗口折并）
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部自行启动的 piik 实例（非本引擎托管）
	StateQuitting State = "quitting" // 预留：收口相折并入 running，本值当前不广播
)

// Options 托管启动参数（构造入参冻结面，B/C 线对接契约）：
// Port/ConfigPath/LogDir 三元组由 service 层裁决后以定值注入——端口经
// "默认 8787 起、被占 +1 扫描 ≤10"分配器试绑；ConfigPath/LogDir 指托管
// 数据目录（上游默认 %APPDATA%\Piik\client.json 不改道则数据外溢用户目录，
// 删版本不干净，托管必须经 CLI 注入改道）。Detached 语义随家族：解除
// JobObject 退出联动（"不随 Hanxi 关闭"，service 层经 !FollowOnExit 决策）。
type Options struct {
	Version    string // 绑定版本 vX.Y.Z（仅入快照，供事件/前端映射）
	Exe        string // piik-app.exe 绝对路径（版本隔离目录内）
	Detached   bool
	Port       int
	ConfigPath string
	LogDir     string
}

func (o Options) validate() error {
	switch {
	case o.Exe == "":
		return fmt.Errorf("piik-app.exe 路径不能为空")
	case o.Port < 1 || o.Port > 65535:
		return fmt.Errorf("监听端口非法: %d", o.Port)
	case o.ConfigPath == "":
		return fmt.Errorf("配置文件路径不能为空")
	case o.LogDir == "":
		return fmt.Errorf("日志目录不能为空")
	}
	return nil
}

// Snapshot 引擎状态快照：事件推送（piik:instance-state）与前端渲染共用同一
// 模型。安全面纪律（联编硬要求，models.go 同款申明）：本类型不得携带口令
// 明文等任何秘密值——上游 "Local access password: X" 行的明文在解析处即弃，
// 这里只有 PasswordSet 布尔。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"` // state==external 时为 true
	Port      int       `json:"port"`     // 本代/最近一代分配端口（0=从未启动）
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`

	// 上游机读字段账（stdout 逐行解析，machine.go）；实例未运行保留最近
	// 一代终值供诊断，新一轮 Start 即清零。
	LocalAccessOpen  bool   `json:"localAccessOpen"`  // "Local access: open" 命中
	PasswordSet      bool   `json:"passwordSet"`      // 口令行命中且值非空（明文已弃）
	LanInvitation    string `json:"lanInvitation"`    // LAN 邀请来源（原样转呈）
	PublicInvitation string `json:"publicInvitation"` // 公网邀请来源（原样转呈）
	NoBrowser        bool   `json:"noBrowser"`        // GATE_NO_BROWSER 命中位
}

// LogEntry 单条实例日志行（service 层如需 piik:instance-log 事件的载荷形状）。
type LogEntry struct {
	Line string `json:"line"`
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
// OnLog 可选：注册即拉起 stdout/stderr 逐行泵并经环形缓冲后转发（与内核
// supervisor.Callbacks.OnLog 同口径——本包自持泵送，行不含换行符，两路间
// 不保证相对顺序）；未注册时不获取输出管道（零开销）。
type Callbacks struct {
	OnState func(snap Snapshot)
	OnLog   func(entry LogEntry)
}

// procSpec 进程创建规格（supervisor.Spec 的服务型裁剪 + stdin 通道需求；
// 本包私有接缝形状，测试注入假 spawner 时同样吃到这些字段以便断言组参）。
type procSpec struct {
	Exe        string
	Args       []string
	Env        []string // 追加到继承环境之后
	WorkingDir string
	HideWindow bool
}

// procHandle 已创建进程的最小抽象（supervisor.processHandle 同形 + Stdin
// 写端——Piik 优雅停机的信令通道，内核因无此面而未选用，见包注释）。
type procHandle interface {
	Start() error
	Wait() error // 阻塞至退出；单一所有者（wait 协程）
	Kill() error // 句柄兜底强杀（不受 PID 复用影响）
	PID() uint32
	ExitCode() int         // Wait 返回后有效；不可得 -1
	Stdin() io.WriteCloser // spawn 前建立的 stdin 写端（nil = 不提供）
	Stdout() io.Reader
	Stderr() io.Reader
}

// spawner 进程创建接缝：默认真实 execSpawner，单测注入假 stdin 管道/假进程。
type spawner func(spec procSpec) (procHandle, error)

// execHandle 真实实现：包装 *exec.Cmd，三管道在 spawn 期（Start 前）建立。
type execHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

// execSpawner 组 exec.Cmd（不经 shell，Args 原样传 CreateProcess）并预建
// stdin/stdout/stderr 管道（exec.Cmd 语义：StdinPipe 等须在 Start 前调用）。
func execSpawner(spec procSpec) (procHandle, error) {
	cmd := exec.Command(spec.Exe, spec.Args...)
	cmd.Dir = spec.WorkingDir
	if len(spec.Env) > 0 {
		// 显式继承当前进程环境再追加（cmd.Env 非 nil 即不再自动继承）。
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if spec.HideWindow {
		applyWindowFlags(cmd)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	return &execHandle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (h *execHandle) Start() error { return h.cmd.Start() }
func (h *execHandle) Wait() error  { return h.cmd.Wait() }
func (h *execHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}
func (h *execHandle) PID() uint32 {
	if h.cmd.Process == nil {
		return 0
	}
	return uint32(h.cmd.Process.Pid)
}
func (h *execHandle) ExitCode() int {
	if h.cmd.ProcessState == nil {
		return -1
	}
	return h.cmd.ProcessState.ExitCode()
}
func (h *execHandle) Stdin() io.WriteCloser { return h.stdin }
func (h *execHandle) Stdout() io.Reader     { return h.stdout }
func (h *execHandle) Stderr() io.Reader     { return h.stderr }

// processRun 当前代运行账目（收口 = Wait 返回且终态已回写）。
type processRun struct {
	h        procHandle
	job      platform.Job
	done     chan struct{} // wait 协程完成终态回写后关闭
	stopping bool          // 手动停止/终止标记：防止终止后误判异常退出（受 mu 保护）
}

// Engine Piik 单实例运行引擎：JobObject 罩住 piik 及其 cloudflared/
// piik-capture 孙进程，stdout/stderr 汇入内存环形日志，机读字段解析入
// 快照；并发纪律照内核（状态锁 mu + 操作锁 startMu 分离、cmd.Wait 唯一
// 所有者、上一代未收口拒绝新启动）。本包零框架依赖。
type Engine struct {
	mu sync.Mutex

	state     State
	version   string
	pid       uint32
	exe       string
	port      int
	errMsg    string
	exitCode  int
	since     time.Time
	stoppedAt time.Time

	// 机读账目（明文口令结构性不存在于任何字段）
	localOpen   bool
	passwordSet bool
	lanOrigin   string
	pubOrigin   string
	noBrowser   bool

	startMu sync.Mutex // Start/Quit/Stop 整段串行
	run     *processRun
	done    chan struct{} // 在途 wait 记账（收口前拒新起）

	jobs  platform.JobAPI
	probe Probe
	spawn spawner
	logs  *ringbuf.RingBuffer
	cb    Callbacks
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe Probe, cb Callbacks) *Engine {
	return &Engine{
		state: StateStopped,
		jobs:  jobAPI,
		probe: probe,
		spawn: execSpawner,
		logs:  ringbuf.New(logCapacity),
		cb:    cb,
	}
}

// buildArgs 上游组参（阶段 0 实锤参数面的单点拼装，纯函数供单测钉死）：
//
//	--local --port <p> --config <托管>/client.json --log-dir <托管>/logs
//
// --local：机读模式随附的本机绑定旗标（实锤组参面）；--config/--log-dir
// 把数据/日志钉死在托管目录（默认 %APPDATA%\Piik 外溢即托管语义破产）。
func buildArgs(opts Options) []string {
	return []string{
		"--local",
		"--port", strconv.Itoa(opts.Port),
		"--config", opts.ConfigPath,
		"--log-dir", opts.LogDir,
	}
}

// buildEnv 机读模式开关：PIIK_CLIENT_GATE_NO_BROWSER=true ——stdout 打机读
// 字段、stdin 收任意字节优雅停机、错误进 stderr 的三件事全由它激活；
// 同时上游"自动拉浏览器"被闸，界面打开归 service 的 OpenWindow 通道。
func buildEnv() []string {
	return []string{"PIIK_CLIENT_GATE_NO_BROWSER=true"}
}

// Start 启动自有实例：组参 → spawn（stdin 写端随句柄建立）→ JobObject
// Create/Assign（Detached 则解除退出联动）→ running 入账 → 双路泵 +
// 监督协程。工作目录锁定主 exe 所在目录（兄弟路径相对解析的兜底保险）。
// 刻意不在 Start 内等就绪：端口/进程双因子归 WaitReady（service 层驱动，
// 超时文案与端口账在其手——dbx/paseo 同款冷启动分工）；启动即退的竞速/
// 抢绑失败由 wait 四分类落 failed/external。
func (e *Engine) Start(opts Options) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	if e.done != nil {
		e.mu.Unlock()
		return errBusy
	}
	e.state = StateStarting
	e.version = opts.Version
	e.port = opts.Port
	e.exe = opts.Exe
	e.pid = 0
	e.exitCode = 0
	e.errMsg = ""
	e.stoppedAt = time.Time{}
	e.since = time.Time{}
	// 新一轮运行：机读账目清零重记（上一代终值不留进新快照）
	e.localOpen, e.passwordSet, e.lanOrigin, e.pubOrigin, e.noBrowser = false, false, "", "", false
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)

	spec := procSpec{
		Exe:        opts.Exe,
		Args:       buildArgs(opts),
		Env:        buildEnv(),
		WorkingDir: filepath.Dir(opts.Exe),
		HideWindow: true, // 控制台子系统程序无窗拉起（frpc 同形，不闪黑框）
	}
	h, err := e.spawn(spec)
	if err != nil {
		e.transition(StateFailed, "进程创建失败: "+err.Error())
		return fmt.Errorf("进程创建失败: %w", err)
	}
	if err := h.Start(); err != nil {
		e.transition(StateFailed, "进程启动失败: "+err.Error())
		return fmt.Errorf("进程启动失败: %w", err)
	}

	run := &processRun{h: h, done: make(chan struct{})}

	// 输出管道须在登记 running 前获取（exec.Cmd 的 StdoutPipe 语义），但泵协程
	// 延后到 Job 绑定成功、running 落账后再拉起——避免 Job 失败触发的
	// abortStart 强杀与泵读同代竞态（内核 Start 同序）。
	var stdoutR, stderrR io.Reader
	if e.cb.OnLog != nil {
		stdoutR, stderrR = h.Stdout(), h.Stderr()
	}

	e.mu.Lock()
	e.run = run
	e.done = run.done
	e.pid = h.PID()
	e.since = time.Now()
	e.mu.Unlock()
	go e.wait(run)

	job, jerr := e.jobs.Create()
	if jerr != nil {
		return e.abortStart(run, fmt.Sprintf("创建 Job Object 失败: %v", jerr), jerr)
	}
	if aerr := job.Assign(h.PID()); aerr != nil {
		_ = job.Close()
		return e.abortStart(run, fmt.Sprintf("JobObject 绑定失败: %v", aerr), aerr)
	}
	if opts.Detached {
		// "不随 Hanxi 关闭"开关：解除退出联动（Quit/Stop 的整树终止仍归 Job 管辖）
		if derr := job.SetAllowKillOnClose(false); derr != nil {
			_ = job.Close()
			return e.abortStart(run, fmt.Sprintf("解除退出联动失败: %v", derr), derr)
		}
	}
	e.mu.Lock()
	run.job = job
	e.mu.Unlock()

	// 先登记 running 再拉泵：极快退出的 wait 有机会落最终状态（frpc 同序）。
	e.transition(StateRunning, "")
	e.startPump(run, stdoutR, true)
	e.startPump(run, stderrR, false)
	return nil
}

// abortStart Start 失败路径统一收口：标记 stopping → 终止 → 等收口 →
// 覆盖 failed 终态（wait 协程因 stopping 先落"已手动停止"，随后被这里的
// failed 覆盖——内核 abortStart 同款事件序列）。
func (e *Engine) abortStart(run *processRun, msg string, cause error) error {
	e.mu.Lock()
	run.stopping = true
	e.mu.Unlock()
	_ = e.killSequence(run)
	settled := waitClosed(run.done, settleTimeout)
	e.transition(StateFailed, msg)
	if !settled {
		return fmt.Errorf("%s（%v；等待进程收口超时）", msg, cause)
	}
	return fmt.Errorf("%s（%v）", msg, cause)
}

// Quit 优雅退出自有实例：向 stdin 写一个换行字节（上游实证收到任意字节
// 优雅停机，内部 5s 预算）→ quitGrace 窗口内等进程自然收口 → 超时
// JobObject Terminate 兜底（整树含 cloudflared/piik-capture 孙进程必杀）。
// 幂等；external/stopped 态无自有进程 = 无操作成功（越权终止外部实例的
// 红线在 wait 分类与 RefreshExternal 两处把守，指引文案归 service 层）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(quitGrace)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 stdin 信令、不耗
// 宽限，直接 JobObject 终止整树（应用退出 Shutdown 通道用，限时返回）。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	run := e.run
	if run != nil {
		run.stopping = true // 先标记：其后 wait 无论何因收尾都归类"手动停止"
	}
	e.mu.Unlock()

	if run == nil {
		return nil // 无自有进程：幂等（external 归属正由 RefreshExternal 甄别呈现）
	}

	// 收口窗口展示层折并入 running（run.stopping 已置位，终态由 wait 的
	// stopped/failed 广播纠正——家族词表口径，无独立 quitting 档）。

	// 优雅通道：仅 Quit（grace>0）且 stdin 可用时投递信令并等自然收口。
	w := run.h.Stdin()
	if grace > 0 && w != nil {
		// 写失败（进程已亡/管道半断）不致命：落入强制终止兜底。
		if _, werr := w.Write([]byte{'\n'}); werr == nil && waitClosed(run.done, grace) {
			_ = w.Close()
			return nil
		}
	}
	// 强制兜底路径：先关 stdin 给上游最后一个 EOF 机会，再整树终止。
	if w != nil {
		_ = w.Close()
	}
	kerr := e.killSequence(run)
	if !waitClosed(run.done, settleTimeout) {
		if kerr != nil {
			return errors.Join(kerr, errors.New("等待 piik 进程退出超时"))
		}
		return errors.New("等待 piik 进程退出超时")
	}
	return nil
}

// killSequence 强制终止序列：job.Terminate 优先（整树语义、不受 PID 复用
// 影响），Job 不可得时兜底句柄强杀（仅自有进程，绝不经端口/名字误伤外部实例）。
func (e *Engine) killSequence(run *processRun) error {
	e.mu.Lock()
	job, h := run.job, run.h
	e.mu.Unlock()

	var termErr error
	if job != nil {
		if termErr = job.Terminate(1); termErr == nil {
			return nil
		}
	}
	if h == nil {
		if termErr != nil {
			return termErr
		}
		return errors.New("instance: 没有可终止的进程")
	}
	return h.Kill()
}

// wait 监督协程：阻塞等待自有进程退出并按家族四分类收尾（顺序不可调换）：
//  1. stopping（手动停止/Start 失败收口）→ stopped；
//  2. 探针仍见目标存活（外部实例在场，我方退让）→ external；
//  3. 退出码 0 → stopped；
//  4. 其余 → failed（附退出码）。
func (e *Engine) wait(run *processRun) {
	err := run.h.Wait()
	code := 0
	if err != nil {
		code = run.h.ExitCode()
	}

	e.mu.Lock()
	stopping := run.stopping
	e.mu.Unlock()

	// 类别 2 探测：瞬时系统调用，失败按"不存在"处理（内核 inspectTimeout 同语义）。
	var extPID uint32
	externalSeen := false
	if !stopping {
		if e.probe.ProcessRunning() {
			externalSeen = true
			if pids := e.probe.FindPIDs(); len(pids) > 0 {
				extPID = pids[0]
			}
		}
	}

	e.mu.Lock()
	if e.run == run { // 当前代才回写（纵深防御）
		e.run = nil
		e.done = nil
		if run.job != nil {
			_ = run.job.Close()
			run.job = nil
		}
	}
	current := e.run == nil && e.done == nil
	switch {
	case !current:
		// 他代在途（理论上 ErrBusy 恒保序，此分支为纵深防御）：只收口不写状态。
		e.mu.Unlock()
		close(run.done)
		return
	case stopping:
		e.state = StateStopped
		e.exitCode = code
		e.pid = 0
		e.errMsg = ""
		e.stoppedAt = time.Now()
	case externalSeen:
		e.state = StateExternal
		e.exitCode = code
		if extPID != 0 {
			e.pid = extPID // 外部实例 PID 是探针事实，可展示
		} else {
			e.pid = 0
		}
		e.exe = ""
		e.errMsg = ""
		e.stoppedAt = time.Now()
	case code == 0:
		e.state = StateStopped
		e.exitCode = 0
		e.pid = 0
		e.errMsg = ""
		e.stoppedAt = time.Now()
	default:
		e.state = StateFailed
		e.exitCode = code
		e.pid = 0
		e.errMsg = fmt.Sprintf("piik 进程异常退出（退出码 %d）。常见原因：端口启动后被抢绑、托管数据目录不可写、版本目录 runtime 兄弟项缺失，详见日志", code)
		e.stoppedAt = time.Now()
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()

	close(run.done)
	e.emit(snap)
}

// startPump 拉起单路日志泵：stdout 路（isStdout=true）先经机读字段解析，
// 两路统一进环形缓冲与 OnLog 事件。代际守卫：非当前代行情丢弃（frpc
// inspectAndWriteLog 同纪律）——上一代残留输出不得污染新一代快照。
// scanner 结束必查 Err：半路故障转为一条可见日志，不静默吞掉。
func (e *Engine) startPump(run *processRun, r io.Reader, isStdout bool) {
	if r == nil {
		return
	}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), 64<<10) // 单行 64KB 上限（内核同口径）
		for sc.Scan() {
			line := sc.Text()
			e.mu.Lock()
			current := e.run == run
			e.mu.Unlock()
			if !current {
				return
			}
			if isStdout {
				e.recordMachineLine(parseMachineLine(line))
				// 口令行明文不落日志链（环形缓冲与 OnLog 事件同经此口）：
				// 快照纪律"秘密不进事件载荷"对日志通道同样成立。
				line = maskSecretLine(line)
			}
			e.logs.Write(line)
			if e.cb.OnLog != nil {
				e.cb.OnLog(LogEntry{Line: line})
			}
		}
		if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
			e.mu.Lock()
			current := e.run == run
			e.mu.Unlock()
			if current {
				e.logs.Write(fmt.Sprintf("…托管进程输出读取中断: %v…", err))
				if e.cb.OnLog != nil {
					e.cb.OnLog(LogEntry{Line: fmt.Sprintf("…托管进程输出读取中断: %v…", err)})
				}
			}
		}
	}()
}

// recordMachineLine 机读字段入账：仅账目真正变化才广播快照（invitation
// origin 一行即终值，不重复推）；口令行的明文已在解析处丢弃。
func (e *Engine) recordMachineLine(u machineUpdate) {
	if !u.changed {
		return
	}
	e.mu.Lock()
	changed := false
	if u.accessHit && u.localOpen != e.localOpen {
		e.localOpen = u.localOpen
		changed = true
	}
	if u.passwordSet && !e.passwordSet {
		// 只增不减：上游改口令经界面重发新行；未命中行（hit=false）绝不参与
		e.passwordSet = true
		changed = true
	}
	if u.lanInvitation != "" && u.lanInvitation != e.lanOrigin {
		e.lanOrigin = u.lanInvitation
		changed = true
	}
	if u.publicInvitation != "" && u.publicInvitation != e.pubOrigin {
		e.pubOrigin = u.publicInvitation
		changed = true
	}
	if u.noBrowser && !e.noBrowser {
		e.noBrowser = true
		changed = true
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()
	if changed {
		e.emit(snap)
	}
}

// RefreshExternal 探针校正 external/stopped 静止态（service 层 5s 轮询 +
// GetStatus 前置调用入口）。仅对静止态生效：running/starting 时探测到的
// 正是自己（优雅停收口窗口的展示层也是 running），会误导状态机（内核同名
// 方法纪律）。刻意不持 startMu：内核同款——GetStatus 是高频轮询面，绝
// 不能排在 Quit 的 6s 宽限窗口之后干等；状态守卫已足。判据为进程名单
// 因子（外部实例端口不可知）。
func (e *Engine) RefreshExternal() {
	e.mu.Lock()
	if e.state == StateRunning || e.state == StateStarting {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	if e.probe.ProcessRunning() {
		var extPID uint32
		if pids := e.probe.FindPIDs(); len(pids) > 0 {
			extPID = pids[0]
		}
		e.mu.Lock()
		if e.state != StateExternal {
			e.state = StateExternal
			e.errMsg = ""
			e.pid = extPID
			e.exe = ""
			e.since = time.Time{}
			e.stoppedAt = time.Now()
		} else {
			e.pid = extPID
		}
		snap := e.snapshotLocked()
		e.mu.Unlock()
		e.emit(snap)
		return
	}

	// 探针确认目标已消失：撤销过期的 external 记录（stopped 态幂等无广播）。
	e.mu.Lock()
	if e.state != StateExternal {
		e.mu.Unlock()
		return
	}
	e.state = StateStopped
	e.pid = 0
	e.since = time.Time{}
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}

// WaitReady 阻塞等待实例就绪（双因子：piik-app.exe 进程 ∧ 分配端口 LISTEN），
// 超时返回 false。端口在 Start 即定格，进程死亡后端口恒不 LISTEN，本方法
// 自然以超时收口（终态由 wait 分类广播，service 层超时文案自持）。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	e.mu.Lock()
	port := e.port
	e.mu.Unlock()

	deadline := time.Now().Add(timeout)
	for {
		if readyFact(e.probe, port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(probePollInterval)
	}
}

// Snapshot 返回当前状态的一致性快照。
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateRunning && e.state != StateStarting {
		return ""
	}
	return e.exe
}

// LocalURL 返回自有实例的本机界面地址（非 running/starting 返回空串）。
// 引擎只提供 URL 组装，浏览器调用归 service 层（"唤窗"语义在无窗服务上
// 的等价物；外部实例端口不可知，本方法不为其背书）。
func (e *Engine) LocalURL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if (e.state != StateRunning && e.state != StateStarting) || e.port == 0 {
		return ""
	}
	return URLForPort(e.port)
}

// Logs 返回最近 n 行进程输出（stdout/stderr 混排）。
func (e *Engine) Logs(n int) []string { return e.logs.Last(n) }

// RunningDuration 自有实例已运行时长（非 running 返回 0）。
func (e *Engine) RunningDuration() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateRunning || e.since.IsZero() {
		return 0
	}
	return time.Since(e.since)
}

// snapshotLocked 前置条件：已持 e.mu。五态直接呈现：优雅停收口相对外
// 部词表无中间档（run.stopping 内部标记承载，窗口内 state 保持 running，
// 与家族 mapState 折并 stopping 的效果一致）。
func (e *Engine) snapshotLocked() Snapshot {
	return Snapshot{
		Version:          e.version,
		State:            e.state,
		PID:              e.pid,
		ExitCode:         e.exitCode,
		Error:            e.errMsg,
		External:         e.state == StateExternal,
		Port:             e.port,
		StartedAt:        e.since,
		StoppedAt:        e.stoppedAt,
		LocalAccessOpen:  e.localOpen,
		PasswordSet:      e.passwordSet,
		LanInvitation:    e.lanOrigin,
		PublicInvitation: e.pubOrigin,
		NoBrowser:        e.noBrowser,
	}
}

// transition 切换状态并广播（回调在锁外执行，防重入死锁）。
func (e *Engine) transition(s State, errMsg string) {
	e.mu.Lock()
	e.state = s
	if s == StateRunning || s == StateStarting {
		e.errMsg = ""
	} else {
		e.errMsg = errMsg
	}
	if s == StateStopped || s == StateFailed {
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}

func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// waitClosed 等待 done 关闭直至超时，返回是否在期限内完成收口。
func waitClosed(done chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
