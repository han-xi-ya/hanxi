// Package instance 实现 Termora 自有实例的进程生命周期托管。
//
// 进程治理主流程（spawn → Job Object 绑定 → 退出分类 → 强制终止兜底）收口至
// 共享内核 hanxi/packages/go/supervisor；本包只保留 Termora 领域适配：
//   - 单实例信使家族（源码实证，见 prober.go 契约注释）：引擎追踪自己拉起的
//     那一个进程；外部实例归 external 感知；"打开窗口"（含自有唤起）走
//     Messenger——无参二次拉起经互斥体 tick 激活在跑实例后自行退出，信使
//     生命周期纪律照 markeron/everything（Start+Release 不 Wait 不进 Job，
//     勿"好心"改成 Wait）；
//   - 二次冷启动撞外部实例的门在 service 层（Launch gate）：若放任 spawn，
//     启动器会 tick 外部实例后 0 码退出——内核终态分类虽能兜住（external
//     接管），但"没建立托管"必须如实告知而不是演一出启动成功；
//   - external 实例退出走 QuitExternal（委托 packages/go/externalquit 按
//     N3 终裁分档执行）：Termora 属"中断有实际损失"的会话类工具（N3 终裁
//     点名 Termora/Termora），恒定 confirm-force 档——强杀前必须经用户
//     明确同意；提权目标 UIPI 拦截如实降级（blocked）；
//   - 分层退出归因（QuitResult）：投递前/强杀前身份复核、WM_CLOSE、宽限
//     窗口均为模块策略；活动会话下 WM_CLOSE 会被 Termora 断开确认框挡住
//     （模态），宽限超时由 JobObject 强杀兜底——自有实例的退出钮即用户
//     "明确要关"的第一人称动作，语义与外部实例的 confirm 闸同构。
//
// 本包零框架依赖，便于单元测试。
package instance

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
	"hanxi/packages/go/externalquit"
	sup "hanxi/packages/go/supervisor"
)

// State 引擎状态机：stopped → starting → running → (stopped | failed)；
// 手动退出窗口内 running/starting/stopping 一律呈现为 quitting。
type State string

const (
	StateStopped  State = "stopped"  // 未运行或已正常退出
	StateStarting State = "starting" // 进程创建与 Job 绑定窗口
	StateRunning  State = "running"  // 自有托管实例运行中
	StateQuitting State = "quitting" // 已发关闭请求、等待退出（宽限期内）
	StateFailed   State = "failed"   // 启动失败或异常退出
	StateExternal State = "external" // 外部自行启动的实例（非本会话托管，N3 分档治理）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限。比看图/截图家族（2~2.5s）
// 宽——Termora 关窗要落盘会话索引与会话确认交互，正常收口耗时上界更高；
// 包级变量供单测压缩，生产 5s。
var closeGracePeriod = 5 * time.Second

// externalGrace QuitExternal 传给执行器的优雅观察宽限（0 = externalquit
// 内核缺省 2.5s）；包级变量仅为单测压缩等待窗口，生产保持零值走缺省。
var externalGrace time.Duration

// manualStopWording 内核手动停止的收口文案；本模块既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。external 态时
// PID/ExePath/StartedAt 为探针实测的外部实例事实（身份不明时保持零值，
// 呈现层如实显示"未取得身份"）。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExePath   string    `json:"exePath"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"`
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析活动版本后填充。
type StartOptions struct {
	Version  string // 绑定版本（展示用）
	Exe      string // Termora.exe 绝对路径（payload 目录内）
	Detached bool   // 独立运行：解除 JobObject 退出联动（"不随 Hanxi 关闭"开关）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Termora.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 Wails 事件推送）。
type Callbacks struct {
	OnState func(Snapshot)
}

// QuitResult Quit 的分层退出结果：Stopped 是否已终止、Forced 是否走了强杀、
// CloseRequested 是否成功投递过 WM_CLOSE、Method 为结果归因。
type QuitResult struct {
	Stopped        bool
	Forced         bool
	CloseRequested bool
	Method         string
}

// noExternalProbe 恒"不在运行"探针：仅作为 NewEngine 未注入探针时的兼容兜底
// （external 分类退化为不可达，回到 W2 之前的管理边界语义）。
type noExternalProbe struct{}

func (noExternalProbe) Inspect(context.Context, uint32) (bool, *platform.ProcInfo, error) {
	return false, nil, nil
}
func (noExternalProbe) MutexHeld() bool                 { return false }
func (noExternalProbe) WaitForReady(time.Duration) bool { return true }
func (noExternalProbe) FocusMainWindow(uint32) bool     { return false }
func (noExternalProbe) FocusAnyWindow() bool            { return false }

// Engine Termora 自有实例运行引擎：组合内核 supervisor.Engine，本层持有
// Termora 专属账目（退出归因标记、宽限收口通道、异常退出码/停止时刻、
// 启动自检锁存文案）。opMu 串行化 Start/Quit 整段操作（内核 startMu 只锁
// 自身原语，分层退出的"投递→观察→复核→强杀"序列必须整段互斥）。
type Engine struct {
	opMu sync.Mutex
	mu   sync.Mutex

	manualExit   bool   // Quit 入口置位：窗口内任何落终都归因手动退出，收口时清零
	quitReported bool   // 已向呈现层报告 quitting
	latchedErr   string // 启动身份自检失败的锁存文案（下次 Start 清除）
	exitCode     int
	stoppedAt    time.Time
	settle       chan struct{} // 进程落终态时关闭；nil=无在途进程

	sup        *sup.Engine
	probe      Probe
	processAPI platform.ProcessAPI
	closeByPID func(uint32) int
	cb         Callbacks

	// launchMessenger 信使拉起接缝（默认真实 spawn，测试注入捕获调用）。
	launchMessenger func(exe string) error
}

// NewEngine 创建引擎（初始 stopped）。Job/Process API 与 closeByPID 的 Win32
// WM_CLOSE 投递实现由内核/平台层承接，单测可替换 closeByPID 替身；
// probe 传 nil 时退化为恒不在场（兼容旧口径，external 态不可达）。
func NewEngine(jobAPI platform.JobAPI, processAPI platform.ProcessAPI, probe Probe, cb Callbacks) *Engine {
	if probe == nil {
		probe = noExternalProbe{}
	}
	// 优雅退出投递走 postCloseFn 接缝（默认真实 Win32 实现，单测注入断言
	// "按目标 PID 收敛"）：用户自行运行的实例窗口绝不代关。
	e := &Engine{probe: probe, processAPI: processAPI, closeByPID: postCloseFn, cb: cb,
		launchMessenger: spawnMessenger}
	e.sup = sup.NewEngine(jobAPI, supProbe{e}, sup.Callbacks{OnState: e.onSupState}).
		WithProcessAPI(processAPI) // 内核兜底强杀走 KillVerified 复核，防 PID 复用误杀
	return e
}

// RefreshExternal 探针校正 external/stopped 静止态（service 层状态查询前置；
// 运行/启动/退出中内核自行短路，不会误探自家进程）。
func (e *Engine) RefreshExternal() { e.sup.RefreshExternal() }

// ExternalRunning 报告当前快照是否为外部实例态。
func (e *Engine) ExternalRunning() bool {
	return e.sup.Snapshot().State == sup.StateExternal
}

// QuitExternal 对当前外部实例执行 N3 终裁分档退出（委托 externalquit）：
// WM_CLOSE 尽力投递 → 宽限观察 → 身份复核 → 按档强杀。Termora 恒定
// PolicyConfirmForce：强杀仅在 Confirm 回调取得用户同意后执行；
// 提权目标（UIPI）如实返回 Method="blocked"，调用方降级为指引。
// 调用前须自查状态为 external；token 身份不全（PID=0）时拒执行——
// 由调用方先行甄别并回指引（不猜身份）。
func (e *Engine) QuitExternal(ctx context.Context, policy externalquit.Policy, risk string, confirm func(string) bool) (QuitResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	snap := e.sup.Snapshot()
	if snap.State != sup.StateExternal {
		return QuitResult{Method: "not-external"}, nil
	}
	if snap.PID == 0 {
		return QuitResult{Method: "probe-missing-pid"}, nil
	}
	token := platform.VerifyToken{PID: snap.PID, ExePath: snap.Exe, StartedAt: snap.Since}
	deps := externalquit.Deps{
		Proc: e.processAPI, Risk: risk, Confirm: confirm, Grace: externalGrace,
		Graceful: func(context.Context) error {
			if e.closeByPID(token.PID) <= 0 {
				return errors.New("未找到可投递关闭消息的窗口")
			}
			return nil
		},
	}
	res, err := externalquit.Quit(ctx, token, policy, deps)
	// 无论成否都让内核复探收口：杀掉→external 撤销落 stopped；杀不动→维持 external。
	e.sup.RefreshExternal()
	return QuitResult{Stopped: res.Stopped, Forced: res.Forced, CloseRequested: res.Method == externalquit.MethodGraceful, Method: res.Method}, err
}

// Start 冷启动自有实例（委托内核：创建进程 → 绑定 JobObject →（按开关）解除
// 退出联动 → running），随后执行模块侧身份自检。已在运行/启动中/退出中时
// 拒绝重复启动。多实例上游拉起即开新窗，不存在信使转发路径。
func (e *Engine) Start(opts StartOptions) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if err := opts.validate(); err != nil {
		return err
	}
	if st := e.sup.Snapshot().State; st == sup.StateRunning || st == sup.StateStarting || st == sup.StateStopping {
		return fmt.Errorf("本会话启动的 Termora 已在运行")
	}
	e.mu.Lock()
	e.manualExit, e.quitReported, e.latchedErr = false, false, ""
	e.exitCode, e.stoppedAt = 0, time.Time{}
	e.mu.Unlock()

	// 全托管统一默认 Detached（"不随 Hanxi 关闭"开关取反传入）：Job 照常绑定
	// 收树，仅解除退出联动；开启联动时 Hanxi 退出/崩溃由 JobObject 连带终止。
	if err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: opts.Detached,
	}); err != nil {
		return err
	}

	// 启动身份自检（模块策略，内核不复核路径）：确认实起进程就是目标 exe。
	// GUI 子系统程序：不隐藏控制台、不做窗口干预（零 fork 承诺）。
	snap := e.sup.Snapshot()
	if snap.State != sup.StateRunning {
		return nil // 竞态：启动即已退出，以内核终态分类为准
	}
	info, err := e.processAPI.Query(snap.PID)
	if err != nil {
		return e.abortFailedStartup("建立进程身份失败: "+err.Error(), fmt.Errorf("建立进程身份失败: %w", err))
	}
	if info.ExePath != "" && !strings.EqualFold(filepath.Clean(info.ExePath), filepath.Clean(opts.Exe)) {
		msg := fmt.Sprintf("启动进程路径不匹配: %s", info.ExePath)
		return e.abortFailedStartup(msg, errors.New(msg))
	}
	return nil
}

// abortFailedStartup 启动自检失败的统一收口：锁存失败文案 → 内核强制终止
// 误起进程 → 静止态改写为 failed 并透传原始错误。
func (e *Engine) abortFailedStartup(latchMsg string, cause error) error {
	e.mu.Lock()
	e.latchedErr = latchMsg
	e.mu.Unlock()
	_ = e.sup.Stop(0)
	outer := e.sup.Snapshot()
	e.mu.Lock()
	snap := e.snapshotLocked(outer)
	e.mu.Unlock()
	e.emit(snap)
	return cause
}

// Focus 把自有托管实例主窗口恢复到前台（按内核登记的自有 PID 过滤，
// 用户自行打开的会话窗口不被打扰）。窗口尚未出现返回 false。
func (e *Engine) Focus() bool {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning {
		return false
	}
	return e.probe.FocusMainWindow(s.PID)
}

// FocusExternal 唤回任一外部 Termora 可见窗口。只切前台，不接管、不绑 Job。
func (e *Engine) FocusExternal() bool {
	return e.probe.FocusAnyWindow()
}

// WaitReady 阻塞等待"可见带标题窗口"出现（领域就绪判据，与内核 Inspect
// 的进程存在性判据不同，保持口径分离）。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// Messenger 无参二次拉起信使：Termora 单实例转发通道（ApplicationSingleton
// 持锁实例收到二次启动即激活主窗口，信使进程自行退出）。external/自有通用、
// 不经内核（信使不属于托管生命周期）。刻意 Start+Release 不 Wait、不进 Job
// ——等待会把 RPC 拖到信使生命周期上，绑 Job 会给"顺手退出"的激活请求挂
// 托管账目（markeron/everything 信使纪律同源，勿"好心"改之）。
func (e *Engine) Messenger(exe string) error {
	return e.launchMessenger(exe)
}

// spawnMessenger 信使真实拉起（Messenger 的默认接缝）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe) // jpackage 启动器按自身目录解析 app/runtime（工作目录无关），锁定同目录纯为行为一致
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起 Termora 激活信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// Quit 分层退出自有实例：身份复核 → 投递 WM_CLOSE → 等待宽限期 → 再复核
// 身份 → 内核强杀兜底。每次动手前 verifyToken 复核 PID 身份（路径+启动时间），
// 不匹配立即拒绝——宁可退出失败也不误杀复用同一 PID 的其他进程。
// 非托管状态返回 Method="not-managed" 不视为错误；external 态走 QuitExternal
// 独立入口（本方法不越权触碰外部实例）。
func (e *Engine) Quit() (QuitResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	snap := e.sup.Snapshot()
	if snap.State != sup.StateRunning && snap.State != sup.StateStarting {
		return QuitResult{Method: "not-managed"}, nil
	}
	e.mu.Lock()
	e.manualExit = true
	settle := e.settle
	e.mu.Unlock()

	if err := e.verifyToken(quitToken(snap)); err != nil {
		if errors.Is(err, platform.ErrProcessNotFound) {
			return QuitResult{Stopped: true, Method: "already-exited"}, nil
		}
		return QuitResult{Method: "ownership-lost"}, fmt.Errorf("进程身份复核失败，已拒绝退出以避免误杀: %w", err)
	}

	e.mu.Lock()
	e.quitReported = true
	e.emit(e.snapshotLocked(snap)) // 先报 quitting 再投递
	e.mu.Unlock()

	requested := e.closeByPID(snap.PID) > 0
	settled := settle == nil // 竞态：门控后进程已收口
	if !settled {
		timer := time.NewTimer(closeGracePeriod)
		select {
		case <-settle:
			settled = true
		case <-timer.C:
		}
		timer.Stop()
	}
	return e.finishQuit(snap, requested, settled)
}

// finishQuit 宽限窗口收口后的归因判定（委托内核强制终止）。
func (e *Engine) finishQuit(snap sup.Snapshot, requested, settled bool) (QuitResult, error) {
	return e.finishQuitWith(snap, requested, settled, e.verifyToken, func() error { return e.sup.Stop(0) })
}

// finishQuitWith 归因决策表（seam 参数供表驱动单测注入假复核/假强杀）：
//
//	settled                        → close-request（宽限窗口内自然收口）
//	verify ErrProcessNotFound      → already-exited（宽限期后发现进程已消失）
//	verify 其他失败                 → ownership-lost + error（拒强杀，防 PID 复用）
//	force nil                      → forced-job（内核整树终止成功）
//	force err                      → forced-process + error（所有终止通道均失败）
func (e *Engine) finishQuitWith(snap sup.Snapshot, requested, settled bool,
	verify func(platform.VerifyToken) error, force func() error) (QuitResult, error) {
	if settled {
		return QuitResult{Stopped: true, CloseRequested: requested, Method: "close-request"}, nil
	}
	if err := verify(quitToken(snap)); err != nil {
		if errors.Is(err, platform.ErrProcessNotFound) {
			return QuitResult{Stopped: true, CloseRequested: requested, Method: "already-exited"}, nil
		}
		return QuitResult{CloseRequested: requested, Method: "ownership-lost"}, fmt.Errorf("强制结束前进程身份已变化，已拒绝操作: %w", err)
	}
	if err := force(); err != nil {
		return QuitResult{CloseRequested: requested, Method: "forced-process"}, err
	}
	return QuitResult{Stopped: true, Forced: true, CloseRequested: requested, Method: "forced-job"}, nil
}

// Stop 立即强杀自有实例（幂等，不投 WM_CLOSE 不耗宽限；联动关闭通道用）。
// 外部实例不受影响（ErrExternal 映射为无操作成功）。
func (e *Engine) Stop() error {
	if err := e.sup.Stop(0); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 态不在管辖范围（归用户，走 QuitExternal 分档）
		}
		return err
	}
	return nil
}

// quitToken 从内核快照重建身份令牌（PID/Exe/StartedAt 与内核登记同源）。
func quitToken(s sup.Snapshot) platform.VerifyToken {
	return platform.VerifyToken{PID: s.PID, ExePath: s.Exe, StartedAt: s.Since}
}

// verifyToken 复核 PID 现身的进程与 token 是否同一实体：路径一致（大小写
// 不敏感）、启动时间 ±1s 内；进程已消失返回 ErrProcessNotFound。
func (e *Engine) verifyToken(token platform.VerifyToken) error {
	current, err := e.processAPI.Query(token.PID)
	if err != nil {
		return platform.ErrProcessNotFound
	}
	if token.ExePath != "" && current.ExePath != "" && !strings.EqualFold(filepath.Clean(token.ExePath), filepath.Clean(current.ExePath)) {
		return platform.ErrTokenMismatch
	}
	if !token.StartedAt.IsZero() && !current.StartedAt.IsZero() {
		diff := token.StartedAt.Sub(current.StartedAt)
		if diff < -time.Second || diff > time.Second {
			return platform.ErrTokenMismatch
		}
	}
	return nil
}

// Snapshot 返回当前状态的一致性快照。
func (e *Engine) Snapshot() Snapshot {
	outer := e.sup.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(outer)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ownPID 返回当前受管进程 PID：仅内核确认自有进程在世（Managed 态）时有效，
// 其余返回 0——WM_CLOSE 钩子与探针归属判定据此收敛。
func (e *Engine) ownPID() uint32 {
	s := e.sup.Snapshot()
	if s.Managed {
		return s.PID
	}
	return 0
}

// ---------- 内核 → termora 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
// 收口记账：终态时提取退出码、落停止时刻、关闭 settle 通道唤醒 Quit。
func (e *Engine) onSupState(s sup.Snapshot) {
	var snap Snapshot
	e.mu.Lock()
	switch s.State {
	case sup.StateStopped, sup.StateFailed:
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			e.exitCode = code
		}
		if e.latchedErr == "" && e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	default: // starting/running/stopping：进程在途，保证 Quit 可等待的收口通道存在
		if e.settle == nil {
			e.settle = make(chan struct{})
		}
	}
	snap = e.snapshotLocked(s)
	if terminal := s.State == sup.StateStopped || s.State == sup.StateFailed; terminal {
		e.manualExit, e.quitReported = false, false
		if e.settle != nil {
			close(e.settle)
			e.settle = nil
		}
	}
	e.mu.Unlock()
	e.emit(snap)
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked(s sup.Snapshot) Snapshot {
	state, errMsg := e.mapKernelState(s)
	pid := s.PID
	switch state {
	case StateStopped, StateFailed:
		pid = 0 // 进程收口后快照 PID 清零
	}
	return Snapshot{
		Version:   s.Version,
		State:     state,
		PID:       pid,
		ExePath:   s.Exe,
		ExitCode:  e.exitCode,
		Error:     errMsg,
		External:  state == StateExternal,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapKernelState 状态词表映射（前置条件：已持 e.mu）：
//
//	supervisor starting/running → starting/running（Quit 窗口内呈现 quitting）
//	supervisor stopping         → quitting（原生有退出中态，直映射）
//	supervisor failed           → failed；手动退出窗口内改判 stopped
//	supervisor stopped          → stopped；启动自检锁存文案在场时改写 failed
//	supervisor external         → external
func (e *Engine) mapKernelState(s sup.Snapshot) (State, string) {
	switch s.State {
	case sup.StateStarting:
		if e.quitReported {
			return StateQuitting, ""
		}
		return StateStarting, ""
	case sup.StateRunning:
		if e.quitReported {
			return StateQuitting, ""
		}
		return StateRunning, ""
	case sup.StateStopping:
		return StateQuitting, ""
	case sup.StateExternal:
		return StateExternal, ""
	case sup.StateFailed:
		if e.manualExit {
			return StateStopped, ""
		}
		return StateFailed, mapKernelError(s.Error)
	default: // stopped
		if e.latchedErr != "" {
			return StateFailed, e.latchedErr
		}
		return StateStopped, mapKernelError(s.Error)
	}
}

// mapKernelError 终态文案：手动停止抹平为空串；内核异常退出消息改写为
// "Termora 异常退出（退出码 N）。若反复出现，请在版本管理重新安装"；
// 其余透传内核措辞。
func mapKernelError(msg string) string {
	switch {
	case msg == "", msg == manualStopWording:
		return ""
	}
	if code, ok := exitCodeFromKernelMessage(msg); ok {
		return fmt.Sprintf("Termora 异常退出（退出码 %d）。若反复出现，请在版本管理重新安装", code)
	}
	return msg
}

// kernelAbnormalExitRe 匹配内核异常退出文案中的退出码。措辞耦合自
// supervisor.wait 的分类消息——内核文案变更时回退为透传 Error，不崩溃仅少改写。
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

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 Termora Probe 适配为内核统一探针契约：回答"是否存在非自有的
// Termora 进程"并携带首个外部实例身份（external 快照展示与 QuitExternal
// 退出令牌的唯一来源）；全部 Query 失败时 ProcInfo=nil——external 在场但
// 身份不明，QuitExternal 据此拒执行回指引。
type supProbe struct{ e *Engine }

func (s supProbe) Inspect(ctx context.Context) (bool, *platform.ProcInfo, error) {
	return s.e.probe.Inspect(ctx, s.e.ownPID())
}
