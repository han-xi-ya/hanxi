package instance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
	sup "hanxi/packages/go/supervisor"
)

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（极短窗口）
	StateRunning  State = "running"  // 自有 Job 托管实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 AirSnip 实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"消息窗尚未创建/截图覆盖层线程占住退出路径"等罕见场。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// settingsFocusWait 投递 CmdSettings 后等待 airsnip.settings 面板在场（并可聚焦）
// 的上限——面板由上游主线程异步创建，投递成功≠窗口已在场。
// overlayWait 同理由，等的是 airsnip.overlay 截图覆盖层（上游实测热键到画面 124ms，
// 2s 上限给足慢机器与首次加载余量）。两个都是包级变量：单测压缩、生产保持 2s。
var (
	settingsFocusWait = 2 * time.Second
	overlayWait       = 2 * time.Second
)

// manualStopWording 内核手动停止的收口文案；airsnip 既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

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
	Exe      string // airsnip.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("airsnip.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine AirSnip 单实例运行引擎：组合内核 supervisor.Engine，本层持有 AirSnip
// 专属账目（退出码/停止时刻）、启动前 config.toml 更新检查接管、内部命令通道
// （截图/贴图/设置）投递与 740 提权特判的广播收口。
//
// 命令通道三个接缝（postCmd/postCloseWin/focusWin）默认指向 Win32 投递实现，
// 单测注入 fake 断言"投了哪个 ID、是否按自有 PID 收窄"——Win32 消息投递本身
// 在 CI 无真 AirSnip 在场时不可验证，不假装验过。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次异常退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	// spawn 失败广播暂扣账目（740 提权特判的收口窗口）：内核 spawn 失败的原始
	// 文案不含提权指引话术，而 elevateHint 需对 Start 返回的错误链做 errors.As
	// 判级（errno 文本随系统语言变化，不可靠字符串匹配）——Start 在途时命中
	// spawn 失败前缀的 failed 广播暂存此处，待 Start 返回后由 wrapper 改写补播。
	holdSpawnFail   bool
	heldSpawnSnap   sup.Snapshot
	heldSpawnSnapOK bool

	sup   *sup.Engine
	probe AirsnipProbe
	cb    Callbacks

	postCmd      func(pid, id uint32) bool
	postCloseWin func(pid uint32) bool
	focusWin     func(pid uint32) bool
	// waitWin 按类名等待上游顶层窗在场（外部判据通道）：PostMessage 返 true 只
	// 证明消息入队，UIPI 丢弃或上游改版吞命令时同样"成功"——唤设置面板这条路
	// 必须看到 airsnip.settings 真出现才算送达。单测注入 fake 免依赖真窗。
	waitWin func(class string, pid uint32, timeout time.Duration) uintptr
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe AirsnipProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:        probe,
		cb:           cb,
		postCmd:      postCommand,
		postCloseWin: postClose,
		focusWin:     focusWindow,
		waitWin:      waitForWindowByClass,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向**自有 PID** 的消息窗投递 WM_CLOSE
	// （上游托盘「退出」项的实现逐字相同：PostMessage(self.hwnd, WM_CLOSE) →
	// WM_DESTROY → PostQuitMessage）。外部实例一律不投（按 pid 收窄）。
	e.sup.SetQuitHook(func(context.Context) error {
		if pid := e.sup.Snapshot().PID; pid != 0 {
			e.postCloseWin(pid)
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；工作目录
// 锁定到 exe 所在目录由内核默认保证）。AirSnip 无 CLI、无启动参数，唯一启动
// 语义即"无参拉起 → 托盘常驻（无主窗口）"。
// 本方法不做单实例探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 四分类
// 兜底（第二实例抢互斥体失败 → 立即退出 → 进程枚举命中外部实例 → external 接管）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 启动前接管上游内置更新检查（见 config.go 的完整理由）：只改 config.toml
	// 的 auto_check_update 单键。失败不阻断启动——最坏回到上游默认行为（会自更新），
	// 与"本模块从未接管"同状，不值得为此打断用户的一次点击（litemonitor 播种同纪律）。
	// 接管属模块策略、刻意留在本层紧邻 spawn（内核不做启动前干预）。
	_ = takeoverUpdateCheck(configPath())

	// spawn 失败广播暂扣开闸（740 特判）：见 Engine holdSpawnFail 注释。
	e.mu.Lock()
	e.holdSpawnFail = true
	e.mu.Unlock()

	// AirSnip release 版是 GUI 子系统程序（#![windows_subsystem = "windows"]），
	// 不产生控制台窗口，故无需 HideWindow，也不做任何窗口干预（零 fork 承诺）。
	err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})

	e.mu.Lock()
	e.holdSpawnFail = false
	held, heldOK := e.heldSpawnSnap, e.heldSpawnSnapOK
	e.heldSpawnSnap, e.heldSpawnSnapOK = sup.Snapshot{}, false
	e.mu.Unlock()
	if heldOK && err != nil {
		// requireAdministrator + 未提权父进程：740 直译为"要求提升"不友好，
		// 改写为指向"以管理员身份运行 Hanxi"的指引文案后补播（词表零漂移，
		// 且必须保留「管理员」关键词——前端 ElevateRestart 据此显隐）。
		if hint := elevateHint(err); hint != "" {
			held.Error = hint
		}
		e.mu.Lock()
		snap := e.snapshotLocked(held)
		e.mu.Unlock()
		e.emit(snap)
	}
	return err
}

// commandTarget 解析命令投递的目标 PID：自有实例在场用它，否则取外部实例的
// 首个 PID（不限属主由 postCommand 内部按 pid==0 兜底为"任一 airsnip.hidden"）。
func (e *Engine) commandTarget() uint32 {
	s := e.sup.Snapshot()
	if (s.State == sup.StateRunning || s.State == sup.StateStarting) && s.PID != 0 {
		return s.PID
	}
	if pids := e.probe.FindPIDs(); len(pids) > 0 {
		return pids[0]
	}
	return 0
}

// Shot 触发一次截图（上游托盘「截图」项 → 冻结屏幕 + 框选覆盖层）。
// **送达判据取外部事实**：投递后等 airsnip.overlay 顶层窗在场——PostMessage 返 true
// 只证明消息入队，UIPI 丢弃、上游改版把菜单 ID 改掉时返回值依旧"成功"，用户只会
// 看到"点了没反应"。窗不在场就报 false，由 service 层给出提权/复探指引。
func (e *Engine) Shot() bool {
	pid := e.commandTarget()
	if !e.postCmd(pid, CmdShot) {
		return false
	}
	return e.waitWin(WindowClassOverlay, pid, overlayWait) != 0
}

// Pin 把剪贴板内容贴到屏幕（上游托盘「从剪贴板贴图」项，热键 F3 的无热键退路）。
func (e *Engine) Pin() bool {
	return e.postCmd(e.commandTarget(), CmdPin)
}

// ShowSettings 打开上游设置面板（托盘左键的同一动作），并尽力把它置前。
// 返回 posted=命令是否送达、focused=是否确认唤起过窗口；posted 为真而
// focused 为假是合法现状（面板已创建但前台锁拒绝对话，或完整性不同被 UIPI 丢）——
// 调用方如实分说，不把它包装成成功。
func (e *Engine) ShowSettings() (posted bool, focused bool) {
	pid := e.commandTarget()
	if !e.postCmd(pid, CmdSettings) {
		return false, false
	}
	// 以"面板真出现"为送达判据（不是 PostMessage 返回值）：窗不在场就直接报
	// posted=false，让 service 给出 UIPI/退出指引，而不是让用户对着空气等待。
	if e.waitWin(WindowClassSettings, pid, settingsFocusWait) == 0 {
		return false, false
	}
	return true, e.focusWin(pid)
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 按自有 PID
// 投递 WM_CLOSE → 宽限 closeGracePeriod 内等进程自然收口 → 超时 JobObject
// 强杀兜底（消息窗未创建/覆盖层线程卡住消息循环等场）。
// 幂等；external 态按既有契约映射为无操作成功（指引文案由 service 层给出）。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待收尾）。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 状态不在管辖范围内：强杀越权，收口由 service 层指引
		}
		return err
	}
	return nil
}

// RefreshExternal 进程枚举校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.sup.RefreshExternal()
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	outer := e.sup.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(outer)
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	return s.Exe
}

// WaitReady 阻塞等待 AirSnip 实例就绪（进程出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ExternalPIDs 外部实例 PID 列表（service 层 external 态指引与诊断用）。
func (e *Engine) ExternalPIDs() []uint32 {
	return e.probe.FindPIDs()
}

// IsMessageWindowPresent 目标消息窗是否在场（命令通道可用性的唯一直接证据）。
// 供 service 在投递后做诚实复探；pid 取 commandTarget 口径。
func (e *Engine) IsMessageWindowPresent() bool {
	return findMessageWindow(e.commandTarget()) != 0
}

// ---------- 内核 → airsnip 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
// spawn 失败广播在 Start 在途时暂扣（740 提权特判的收口窗口），其余照常映射转发。
func (e *Engine) onSupState(s sup.Snapshot) {
	e.mu.Lock()
	if e.holdSpawnFail && isSpawnFailure(s) {
		e.heldSpawnSnap, e.heldSpawnSnapOK = s, true
		e.mu.Unlock()
		return
	}
	snap := e.snapshotLocked(s)
	e.mu.Unlock()
	e.emit(snap)
}

// isSpawnFailure 判定内核快照是否为"进程创建/启动失败"类消息（spawn 路径的
// failed 广播）。措辞耦合自 supervisor.Start 的两处 transition 前缀——内核文案
// 变更时本判定退化为不暂扣（原始透传广播照常送达），仅丢失 740 改写层，不崩溃。
func isSpawnFailure(s sup.Snapshot) bool {
	return s.State == sup.StateFailed &&
		(strings.HasPrefix(s.Error, "进程启动失败: ") || strings.HasPrefix(s.Error, "进程创建失败: "))
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked(s sup.Snapshot) Snapshot {
	switch s.State {
	case sup.StateStopped, sup.StateFailed:
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
		Version:   s.Version,
		State:     mapState(s.State),
		PID:       s.PID,
		ExitCode:  e.exitCode,
		Error:     mapErrorMessage(s),
		External:  s.State == sup.StateExternal,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（airsnip 既有词表无 stopping：终止窗口对前端
//	                      保持运行语义，收口后由 stopped/failed 终态广播纠正）
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

// mapErrorMessage 还原 airsnip 既有失败文案：
//   - 内核异常退出消息改回"AirSnip 异常退出（退出码 N）……"并如实预告两条上游
//     门槛（管理员权限与未代码签名）——文案含「管理员」关键词，与 740 直拒
//     路径共用前端一键提权重启钮；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("AirSnip 异常退出（退出码 %d）。AirSnip 需以管理员权限运行，Hanxi 未提权时请以管理员身份重启 Hanxi；"+
				"上游 exe 未做代码签名，首次运行被 SmartScreen 拦时请选「更多信息 → 仍要运行」；也可尝试重新安装该版本", code)
		}
	}
	if s.State == sup.StateStopped && s.Error == manualStopWording {
		return ""
	}
	return s.Error
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 AirsnipProbe（进程快照枚举）适配为内核统一探针契约。
// AirSnip 的互斥体在提权进程名下、跨完整性可读性无实证（见 prober.go），
// 进程名 airsnip.exe 是唯一稳定标识；本探针能给出存活 PID——Inspect 附带
// 首个命中进程的 ProcInfo，内核 external 入账时据此充实快照 PID。
// 快照枚举为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错）。
type supProbe struct{ p AirsnipProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	pids := s.p.FindPIDs()
	if len(pids) == 0 {
		return false, nil, nil
	}
	return true, &platform.ProcInfo{PID: pids[0], Name: exeName}, nil
}
