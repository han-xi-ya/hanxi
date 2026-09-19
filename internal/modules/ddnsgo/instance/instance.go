package instance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/ringbuf"
	sup "hanxi/packages/go/supervisor"
)

// logCapacity 内存环形日志容量（行）。
const logCapacity = 1000

// LogCapacityHint 对外暴露环形缓冲容量，供 service 层截断"取最近 N 行"上界。
const LogCapacityHint = logCapacity

const (
	// daemonEnvKey/daemonEnvVal 上游 main.go 实证的后门：DDNS_GO_DAEMON=1
	// 跳过 kardianos/service 状态检测直跑 run()。用户若曾用 `-s install`
	// 装过同名 Windows 服务，裸启动 exe 会被劫持进 SCM 路径立即失败退出，
	// 注入该变量后此雷区对托管启动免疫。经 Spec.Env 交内核注入子进程。
	daemonEnvKey = "DDNS_GO_DAEMON"
	daemonEnvVal = "1"

	// configFileName 上游配置恒存位置：%USERPROFILE%\.ddns_go_config.yaml
	// （util/user.go GetConfigFilePathDefault）。
	configFileName = ".ddns_go_config.yaml"
)

// readyTimeout Start 后等待 web 端口就绪的上限（→ Spec.ReadyTimeout）。
// 端口绑定失败的进程会存活一分钟才退出（上游特性），就绪等待不能以
// "进程还活着"作成功信号——就绪判定经 supProbe.Inspect 的端口语义收口。
var readyTimeout = 20 * time.Second

// quitGrace Quit 的优雅窗口预算（→ 内核 Stop(grace)）：窗口内先跑 quitHook
// （配置写静默期，上限 configSettleMax）；钩子返回错误后内核直接进入
// JobObject 终止并等待收口（settle 兜底在内核侧）。预算须覆盖静默上限，
// 取代旧 quitSettleWait 手写收口等待。
var quitGrace = 8 * time.Second

// errNoGracefulExit quitHook 静默期走完后的固定回执：上游无退出信令可投递，
// 告知内核"优雅通道无法达成自然退出"，随即进入强制终止。
var errNoGracefulExit = errors.New("ddns-go 无优雅退出信令，静默期后直接终止")

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

// Engine ddns-go 单实例运行引擎：组合内核 supervisor.Engine，
// 进程治理主流程（spawn → JobObject 绑定 → 就绪/退出分类 → 终止收口）
// 全部委托内核；本层保留 ddnsgo 领域适配：
//   - 端口预检与双通道探针（readiness=TCP 端口 / external=进程名扫描）；
//   - 日志环形缓冲与凭据脱敏（内核 OnLog 只逐行搬运，策略在本层回调内）；
//   - 状态词表/错误文案映射、退出码与停止时刻账目、端口占用 failed 叠加态。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻
	listen    string    // 最近一次 Start 绑定的监听地址（快照常显，沿用旧口径）
	portErr   string    // 端口预检 failed 覆盖文案（仅内核静止 stopped 态期间有效）

	startMu  sync.Mutex  // Start/Stop/Quit 互斥临界区（预检与内核操作整段串行）
	starting atomic.Bool // 探针相态开关：内核 Start 窗口内 = 端口就绪语义

	sup   *sup.Engine
	probe Probe
	logs  *ringbuf.RingBuffer
	cb    Callbacks

	// supStart 内核 Start 接缝（默认真实委托；单测打桩断言 Spec 透传/绕开真进程）。
	supStart func(ctx context.Context, spec sup.Spec) error
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe Probe, cb Callbacks) *Engine {
	e := &Engine{
		probe: probe,
		logs:  ringbuf.New(logCapacity),
		cb:    cb,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{e}, sup.Callbacks{
		OnState: e.onSupState,
		// OnLog 注册即令内核拉起 stdout/stderr 逐行泵；敏感信息脱敏与
		// 环形缓冲策略按内核包注释口径留在本层回调（e.onSupLog）内。
		OnLog: e.onSupLog,
	})
	// Quit 双通道的优雅侧：上游无退出信令可投递，"优雅"唯一能保护的是
	// 配置写窗口（SaveConfig 为裸 os.WriteFile 非原子，终止不得撞上）。
	// 钩子只静默、不指挥退出，静默毕回错 → 内核立即 JobObject 强制终止；
	// Stop(0) 则整体跳过本钩子——"Quit 等收尾、Stop 不等"的语义差保持。
	e.sup.SetQuitHook(func(context.Context) error {
		quiesceConfigWrite()
		return errNoGracefulExit
	})
	e.supStart = e.sup.Start
	return e
}

// Start 启动自有实例并等待 web 端口就绪：端口预检（模块自持，内核 Spec
// 无启动前挂点）→ 委托内核（隐藏控制台拉起、注入 DDNS_GO_DAEMON=1、
// JobObject 绑定、按 supProbe 端口语义轮询就绪）。
// 就绪失败内核会主动终止"僵活"进程并落 failed——上游端口冲突时进程
// 会存活一分钟才退出，不能以进程存活为成功信号。
func (e *Engine) Start(opts StartOptions) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.listen = opts.ListenAddr
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.portErr = ""
	e.mu.Unlock()

	// 端口预检：已被占用时先甄别是否外部 ddns-go（进程名扫描），
	// 避免冷启动竞速误伤（TOCTOU 残余窗口交给内核就绪轮询兜底）。
	if e.probe.PortOpen(opts.ListenAddr) {
		if e.probe.IsRunning() {
			e.sup.RefreshExternal() // 内核 external 入账并广播（静止态生效）
			return fmt.Errorf("检测到 ddns-go 已在 %s 运行（非 Hanxi 托管），请先在其窗口/服务中退出", opts.ListenAddr)
		}
		e.failPortOccupied(opts.ListenAddr)
		return fmt.Errorf("端口 %s 已被占用", opts.ListenAddr)
	}

	e.starting.Store(true)
	defer e.starting.Store(false)

	// 窗口干预经内核 Spec 声明（child_windows 蓝本上收）：ddns-go 是控制台
	// 子系统程序，HideWindow=true 由内核注入 CREATE_NO_WINDOW 不闪黑窗。
	err := e.supStart(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          []string{"-l", opts.ListenAddr}, // 回环绑定，面板不外露局域网
		Env:           []string{daemonEnvKey + "=" + daemonEnvVal},
		HideWindow:    true,
		ReadyTimeout:  readyTimeout,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
	if err != nil {
		return rewriteStartError(err, opts.ListenAddr)
	}
	return nil
}

// Quit 退出自有实例（幂等；external/stopped 无自有进程 → 无操作成功，
// 指引文案由 service 层给出）：委托内核 Stop(quitGrace)——窗口内先行
// quitHook（配置写静默期），静默毕内核强制终止并等待收口。
// 与 Stop 的语义差保留：Stop(grace=0) 不跑钩子、不等静默期。
func (e *Engine) Quit() error {
	return e.stopWith(quitGrace)
}

// Stop 立即强杀自有实例（幂等）。应用退出通道（OnShutdown）走此路径：
// 必须限时返回，不能让用户主程序等待配置写静默期。
func (e *Engine) Stop() error {
	return e.stopWith(0)
}

// stopWith 统一停止通道：external 不在管辖范围（内核回 ErrExternal）
// 按 ddnsgo 既有契约映射为无操作成功；静止态内核自行幂等。
func (e *Engine) stopWith(grace time.Duration) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil
		}
		return err
	}
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

// configSettleQuiet / configSettleMax 配置写静默期防护参数（Quit 前生效）：
// 上游 SaveConfig 为裸 os.WriteFile（非原子 tmp+rename），强杀恰好撞上
// webui 保存会截毁用户配置文件。mtime 距今小于 Quiet 视为"正在保存"，
// 推迟终止直至静默或到达 Max 上限。包级变量便于单测压缩时长。
var (
	configSettleQuiet = 1500 * time.Millisecond
	configSettleMax   = 5 * time.Second
)

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

// RefreshExternal 探测外部实例校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.sup.RefreshExternal()
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	return e.snapshotOf(e.sup.Snapshot())
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	return s.Exe
}

// ListenAddr 返回自有实例监听地址（未运行返回空串）。
func (e *Engine) ListenAddr() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.listen
}

// PortOpen 代理探测指定地址是否已有 ddns-go web 服务（external 场景定位面板）。
func (e *Engine) PortOpen(addr string) bool { return e.probe.PortOpen(addr) }

// Logs 返回最近 n 行进程输出。
func (e *Engine) Logs(n int) []string { return e.logs.Last(n) }

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → ddnsgo 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
// 任何非 stopped 广播即宣告新一轮运行开始，撤销端口预检的 failed 叠加文案。
func (e *Engine) onSupState(s sup.Snapshot) {
	if s.State != sup.StateStopped {
		e.mu.Lock()
		e.portErr = ""
		e.mu.Unlock()
	}
	e.emit(e.snapshotOf(s))
}

// snapshotOf 内核快照 + 本包账目 → ddnsgo Snapshot。
func (e *Engine) snapshotOf(s sup.Snapshot) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(s)
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked(s sup.Snapshot) Snapshot {
	state := mapState(s.State)
	errMsg := e.mapErrorMessage(s)
	if s.State == sup.StateStopped && e.portErr != "" {
		// 端口预检失败叠加：内核侧仍是 stopped，契约面如实呈现 failed。
		state, errMsg = StateFailed, e.portErr
	}
	switch state {
	case StateStopped, StateFailed:
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	}
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			e.exitCode = code
		}
	}
	return Snapshot{
		Version:    s.Version,
		State:      state,
		PID:        s.PID,
		ExitCode:   e.exitCode,
		Error:      errMsg,
		External:   s.State == sup.StateExternal,
		ListenAddr: e.listen,
		StartedAt:  s.Since,
		StoppedAt:  e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（ddnsgo 既有词表无 stopping：终止窗口对前端保持
//	                      运行语义，收口后由 stopped/failed 终态广播纠正）
//	supervisor external → external
//	supervisor failed   → failed
func mapState(s sup.State) State {
	switch s {
	case sup.StateStarting:
		return StateStarting
	case sup.StateRunning, sup.StateStopping:
		return StateRunning
	case sup.StateExternal:
		return StateExternal
	case sup.StateFailed:
		return StateFailed
	default:
		return StateStopped
	}
}

// kernelAbnormalExitRe 匹配内核异常退出文案中的退出码。措辞耦合自
// supervisor.wait 的分类消息"托管进程异常退出（退出码 %d）"——内核文案变更时
// 本处回退为透传 Error（ExitCode 保持账目值），不会崩溃，仅少一层改写。
var kernelAbnormalExitRe = regexp.MustCompile(`托管进程异常退出（退出码 (-?\d+)）`)

// exitCodeFromKernelMessage 从内核异常退出文案中提取退出码。
func exitCodeFromKernelMessage(msg string) (int, bool) {
	g := kernelAbnormalExitRe.FindStringSubmatch(msg)
	if g == nil {
		return 0, false
	}
	var code int
	if _, err := fmt.Sscanf(g[1], "%d", &code); err != nil {
		return 0, false
	}
	return code, true
}

// readyAbortPhrase 内核就绪失败文案的识别锚（"启动后实例…未就绪"，超时与
// 进程早退两条分类共用）；内核措辞变更时退化为透传，仅少一层改写。
const readyAbortPhrase = "未就绪"

// mapErrorMessage 还原 ddnsgo 既有失败文案（前置条件：已持 e.mu）：
//   - 内核就绪失败 → "启动后端口 %s 始终不可用（…），已终止"；
//   - 内核异常退出 → "ddns-go 异常退出（退出码 N）。常见原因：…"；
//   - 手动停止的"已手动停止"按 ddnsgo 既有口径清空（旧引擎停止路径从不外发
//     该文案，区别于 markeron 的历史透传）；其余透传。
func (e *Engine) mapErrorMessage(s sup.Snapshot) string {
	switch {
	case s.State == sup.StateStopped && s.Error == "已手动停止":
		return ""
	case s.State == sup.StateFailed && strings.Contains(s.Error, readyAbortPhrase):
		return fmt.Sprintf("启动后端口 %s 始终不可用（可能有其他进程抢绑，或上游检测到同名服务环境异常），已终止", e.listen)
	case s.State == sup.StateFailed:
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("ddns-go 异常退出（退出码 %d）。常见原因：端口 %s 启动后被抢绑、配置文件不可写，详见日志", code, e.listen)
		}
	}
	return s.Error
}

// rewriteStartError 把内核 Start 返回错误改写回 ddnsgo 既有口径：就绪失败
// 路径旧引擎返回值与快照 Error 同文（service 层直接 %w 上抛给用户）；
// 其余（Job 创建失败等）内核文案已与既有措辞一致，透传。
func rewriteStartError(err error, addr string) error {
	if strings.Contains(err.Error(), readyAbortPhrase) {
		return fmt.Errorf("启动后端口 %s 始终不可用（可能有其他进程抢绑，或上游检测到同名服务环境异常），已终止", addr)
	}
	return err
}

// failPortOccupied 端口被非 ddns-go 程序占用的预检失败落账：内核无对外
// "直落 failed"挂点，本层以叠加文案覆盖内核静止态（事件载荷形状不变；
// 任何非 stopped 内核广播撤销叠加，见 onSupState）。
func (e *Engine) failPortOccupied(addr string) {
	e.mu.Lock()
	e.portErr = fmt.Sprintf("端口 %s 已被其他程序占用，可在设置中更换监听端口", addr)
	snap := e.snapshotLocked(e.sup.Snapshot())
	e.mu.Unlock()
	e.emit(snap)
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// onSupLog 内核逐行转发的子进程输出 → ddnsgo 既有日志链：
// 脱敏 → 环形缓冲 → 事件回调（内核只搬运不加工的口径见 supervisor 包注释；
// 行内容不含换行符，旧泵通道的行尾换行随本迁移归一去除）。
func (e *Engine) onSupLog(line string) {
	line = scrubSecrets(line)
	e.logs.Write(line)
	if e.cb.OnLog != nil {
		e.cb.OnLog(LogEntry{Line: line})
	}
}

// supProbe 把 ddnsgo 的双通道探测适配为内核统一探针契约。
// ddnsgo 的存活判定（进程名扫描，覆盖任意端口/服务形态实例）与就绪判定
// （自有监听地址 TCP 可连）语义不同，内核 Inspect 是单一入口——以启动窗口
// 相态区分：sup.Start 在途报端口就绪（"进程活着"不作成功信号，上游端口
// 冲突僵活一分钟特性所迫）；窗口外报进程扫描（external 甄别保持旧口径）。
// 探测为瞬时调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 不产出 ProcInfo（沿用原引擎不取外部实例 PID 的口径）。
type supProbe struct{ e *Engine }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	if s.e.starting.Load() {
		s.e.mu.Lock()
		addr := s.e.listen
		s.e.mu.Unlock()
		return addr != "" && s.e.probe.PortOpen(addr), nil, nil
	}
	return s.e.probe.IsRunning(), nil, nil
}

// secretQueryRe ddns-go 的 DNS 服务商凭据多以 URL query / 表单字段形态参与请求，
// 异常栈与调试日志可能整串带出。通用脱敏：常见敏感参数名的取值置换为 ***。
// （上游 web 日志页有自带脱敏，但 stdout 通道没有，托管侧必须自行设防。）
var secretQueryRe = regexp.MustCompile(`(?i)((?:token|secret|password|passwd|access[_-]?key(?:[_-]?id|[_-]?secret)?|accesskey|apikey|api_key|ak|sig|signature)=)[^&\s"']+`)

func scrubSecrets(line string) string {
	return secretQueryRe.ReplaceAllString(line, "${1}***")
}
