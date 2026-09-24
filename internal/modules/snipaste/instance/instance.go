// Package instance 实现 Snipaste 自有实例的进程生命周期托管（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 退出分类 → 强制终止兜底）收口至
// 共享内核 hanxi/packages/go/supervisor；本包只保留 Snipaste 领域适配：
//   - 外部实例探针（W2/N1，2026-09-21 起）：进程名枚举探测外部 Snipaste，
//     状态词表含 external 态；外部实例的退出走本包 QuitExternal（委托
//     packages/go/externalquit 按 N3 终裁分档执行），内核 Stop 对 external
//     依旧只甄别不触碰（ErrExternal 边界不变）；
//   - 分层退出归因（QuitResult）：退出前身份复核、WM_CLOSE 投递、宽限窗口、
//     强杀前身份复核均为内核未表达的模块策略，由本层驱动；内核 Stop(0) 仅承担
//     最后一层强制终止（job.Terminate 整树语义 → KillVerified 兜底）；
//   - 恒脱管：Snipaste 设计上跨 Hanxi 生命周期存活，Start 固定
//     Spec.DetachFromJob=true（SetAllowKillOnClose(false)），页面手动 Quit
//     是唯一收尾入口；
//   - 启动身份自检：Query 实起进程路径核对（防启动器转发到意外进程）属模块
//     策略，内核不复核路径；
//   - 状态词表与快照形状映射：内核 stopping→quitting、"已手动停止"文案抹平、
//     异常退出码提取回写、退出收口后 PID 清零、startup 自检失败文案锁存。
//
// 本包零框架依赖，便于单元测试。
package instance

import (
	"context"
	"errors"
	"fmt"
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

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限；包级变量供单测压缩，生产 2.5s。
var closeGracePeriod = 2500 * time.Millisecond

// Snapshot 引擎状态快照，事件推送与前端渲染共用；external 态时 PID/ExePath
// 为探针实测的外部实例事实（版本/启动时刻同样如实展示）。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExePath   string    `json:"exePath"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析活动版本后填充。
type StartOptions struct {
	Version string // 绑定版本（展示用）
	Exe     string // Snipaste.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Snipaste.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 Wails 事件推送）。
type Callbacks struct {
	OnState func(Snapshot)
}

// QuitResult Quit 的分层退出结果：Stopped 是否已终止、Forced 是否走了强杀、
// CloseRequested 是否成功投递过 WM_CLOSE、Method 为结果归因（close-request/forced-job/...）。
type QuitResult struct {
	Stopped        bool
	Forced         bool
	CloseRequested bool
	Method         string
}

// noExternalProbe 恒"不在运行"探针：仅作为 NewEngine 未注入探针时的兼容兜底
// （external 分类退化为不可达，回到 W2 之前的管理边界语义）。
type noExternalProbe struct{}

func (noExternalProbe) Inspect(context.Context) (bool, *platform.ProcInfo, error) {
	return false, nil, nil
}

// Engine Snipaste 自有实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 Snipaste 专属账目（退出归因标记、宽限收口通道、异常退出码/停止
// 时刻、启动自检锁存文案）。opMu 串行化 Start/Quit 整段操作（内核 startMu
// 只锁自身原语，分层退出的"投递→观察→复核→强杀"序列必须整段互斥）。
type Engine struct {
	opMu sync.Mutex
	mu   sync.Mutex

	manualExit   bool   // Quit 入口置位（承原 stopping 语义）：窗口内任何落终都归因手动退出，收口时清零
	quitReported bool   // 已向呈现层报告 quitting（原 transition(StateQuitting) 时点）
	latchedErr   string // 启动身份自检失败的锁存文案（下次 Start 清除）
	exitCode     int
	stoppedAt    time.Time
	settle       chan struct{} // 进程落终态时关闭；nil=无在途进程

	sup        *sup.Engine
	processAPI platform.ProcessAPI
	closeByPID func(uint32) int
	cb         Callbacks
}

// NewEngine 创建引擎（初始 stopped）。Job/Process API 与 closeByPID 的
// Win32 WM_CLOSE 投递实现由内核/平台层承接，单测可替换 closeByPID 替身；
// probe 传 nil 时退化为恒不在场（兼容旧口径，external 态不可达）。
func NewEngine(jobAPI platform.JobAPI, processAPI platform.ProcessAPI, probe Probe, cb Callbacks) *Engine {
	if probe == nil {
		probe = noExternalProbe{}
	}
	e := &Engine{processAPI: processAPI, closeByPID: postCloseByPID, cb: cb}
	e.sup = sup.NewEngine(jobAPI, probe, sup.Callbacks{OnState: e.onSupState}).
		WithProcessAPI(processAPI) // 内核兜底强杀走 KillVerified 复核，防 PID 复用误杀
	return e
}

// RefreshExternal 探针校正 external/stopped 静止态（service 层状态查询前置/
// 轮询入口；运行/启动/退出中内核自行短路，不会误探自家进程）。
func (e *Engine) RefreshExternal() { e.sup.RefreshExternal() }

// ExternalRunning 报告当前快照是否为外部实例态。
func (e *Engine) ExternalRunning() bool {
	return e.sup.Snapshot().State == sup.StateExternal
}

// QuitExternal 对当前外部实例执行 N3 终裁分档退出（委托 externalquit）：
// WM_CLOSE 尽力投递 → 宽限观察 → 身份复核 → 按档强杀（force-free 直杀 /
// confirm-force 经 Confirm 回调同意后杀）。提权目标（UIPI）如实返回
// Method="blocked"，调用方降级为指引。调用前须自查状态为 external。
func (e *Engine) QuitExternal(ctx context.Context, policy externalquit.Policy, risk string, confirm func(string) bool) (QuitResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	snap := e.sup.Snapshot()
	// 守卫（非 external / 身份不全拒执行）已收编 QuitExternalOf：PID=0 时
	// Query(0) 失败误归因 already-exited 谎报"已退出"的坑由入口统一挡住。
	res, err := externalquit.QuitExternalOf(ctx, externalquit.ExternalQuitRequest{
		External:  snap.State == sup.StateExternal,
		PID:       snap.PID,
		ExePath:   snap.Exe,
		StartedAt: snap.Since,
		Policy:    policy,
		Risk:      risk,
		Confirm:   confirm,
		Proc:      e.processAPI,
		Graceful: func(_ context.Context, pid uint32) error {
			if e.closeByPID(pid) <= 0 {
				return errors.New("未找到可投递关闭消息的窗口")
			}
			return nil
		},
	})
	// 无论成否都让内核复探收口：杀掉→external 撤销落 stopped；杀不动→维持 external。
	e.sup.RefreshExternal()
	return QuitResult{Stopped: res.Stopped, Forced: res.Forced, Method: res.Method}, err
}

// Start 冷启动自有实例（委托内核：创建进程 → 绑定 JobObject → 解除退出联动 →
// running），随后执行模块侧身份自检。已在运行/启动中/退出中时拒绝重复启动。
func (e *Engine) Start(opts StartOptions) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if err := opts.validate(); err != nil {
		return err
	}
	if st := e.sup.Snapshot().State; st == sup.StateRunning || st == sup.StateStarting || st == sup.StateStopping {
		return fmt.Errorf("本会话启动的 Snipaste 已在运行")
	}
	e.mu.Lock()
	e.manualExit, e.quitReported, e.latchedErr = false, false, ""
	e.exitCode, e.stoppedAt = 0, time.Time{}
	e.mu.Unlock()

	// Snipaste 设计上恒脱管：绑定 Job 后立即解除退出联动（Hanxi 退出/崩溃不
	// 连带结束，页面手动 Quit 才负责收尾），与"不随 Hanxi 关闭"开关同形。
	if err := e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		DetachFromJob: true,
	}); err != nil {
		return err
	}

	// 启动身份自检（模块策略，内核不复核路径）：确认实起进程就是目标 exe，
	// 防启动器转发到意外进程；失败即经内核强制终止并锁存 failed 文案。
	// 与原实现的口径差异：自检发生在 Job 绑定之后（原来在绑定之前、以裸
	// Kill 收场），误起进程同样被整树终止，只是事件流多一次短暂 running。
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

// abortFailedStartup 启动自检失败的统一收口：先锁存失败文案（内核收口广播据
// 此跳过停止时刻记账，还原原实现"自检失败不落 stoppedAt"的语义），再经内核
// 强制终止误起进程，最后把静止态改写为 failed 并透传原始错误。
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

// Quit 分层退出（模块层驱动归因，内核只承担最后一层强制终止）：
// 身份复核 → 投递 WM_CLOSE → 等待宽限期 → 再复核身份 → sup.Stop(0) 强杀。
// 每次动手前 verifyToken 复核 PID 身份（路径+启动时间），身份不匹配立即拒绝
// 并报错——宁可退出失败也不误杀复用同一 PID 的其他进程。
// 非托管状态返回 Method="not-managed" 不视为错误；external 态的退出走
// QuitExternal 独立入口（本方法不越权触碰外部实例）。
func (e *Engine) Quit() (QuitResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	snap := e.sup.Snapshot()
	if snap.State != sup.StateRunning && snap.State != sup.StateStarting {
		return QuitResult{Method: "not-managed"}, nil
	}
	e.mu.Lock()
	e.manualExit = true // 先于一切置位（承原 stopping 语义）：此后任何落终都归因手动退出
	settle := e.settle
	e.mu.Unlock()

	// 动手前身份复核：进程已消失按"已退出"归因（不投 WM_CLOSE），身份不符拒操作。
	if err := e.verifyToken(quitToken(snap)); err != nil {
		if errors.Is(err, platform.ErrProcessNotFound) {
			return QuitResult{Stopped: true, Method: "already-exited"}, nil
		}
		return QuitResult{Method: "ownership-lost"}, fmt.Errorf("进程身份复核失败，已拒绝退出以避免误杀: %w", err)
	}

	e.mu.Lock()
	e.quitReported = true
	e.emit(e.snapshotLocked(snap)) // 原 transition(StateQuitting, "")：先报 quitting 再投递
	e.mu.Unlock()

	requested := e.closeByPID(snap.PID) > 0
	settled := settle == nil // 竞态：门控后进程已收口（原 select 即刻命中 waitDone）
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
//	settled                        → close-request（进程在宽限窗口内自然收口）
//	verify ErrProcessNotFound      → already-exited（宽限期后才发现进程已消失）
//	verify 其他失败                 → ownership-lost + error（拒强杀，防 PID 复用误伤）
//	force nil                      → forced-job（内核整树终止成功，含 KillVerified 兜底成功）
//	force err                      → forced-process + error（所有终止通道均失败）
//
// 与原实现的口径差异（内核把强制通道收口为一个 Stop 调用，无法回报具体是哪
// 层成功）：原 forced-process 仅在"job.Terminate 失败、KillVerified 兜底成功"
// 时出现——该罕见成功态现统一记为 forced-job（用户可见语义一致）；失败态仍
// 记 forced-process + error。
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

// quitToken 从内核快照重建身份令牌：PID/Exe/StartedAt 均在 Start 登记时经
// Query 建立（与内核内部 VerifyToken 同源），本层复核用同一形状。
func quitToken(s sup.Snapshot) platform.VerifyToken {
	return platform.VerifyToken{PID: s.PID, ExePath: s.Exe, StartedAt: s.Since}
}

// verifyToken 复核 PID 现身的进程与 token 是否同一实体：路径需一致（大小写不敏感），
// 启动时间差超 ±1s 视为 PID 复用。进程已消失返回 ErrProcessNotFound（调用方按"已退出"处理）。
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

// ---------- 内核 → snipaste 形状映射 ----------

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
	// 映射先行（failed+manualExit→stopped 的归因改写依赖标记），再清账并收口。
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
		pid = 0 // 原语义：进程收口后快照 PID 清零
	}
	return Snapshot{
		Version:   s.Version,
		State:     state,
		PID:       pid,
		ExePath:   s.Exe,
		ExitCode:  e.exitCode,
		Error:     errMsg,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapKernelState 状态词表映射（前置条件：已持 e.mu）：
//
//	supervisor starting/running → starting/running（Quit 窗口内呈现 quitting）
//	supervisor stopping         → quitting（Snipaste 词表原生有退出中态，直映射）
//	supervisor failed           → failed；手动退出窗口内改判 stopped（承原
//	                              stopping 先置位语义：Quit 期间任何落终都算手动退出）
//	supervisor stopped          → stopped；启动自检锁存文案在场时改写为 failed
//	                              （还原原实现身份/路径自检失败的落点）
//	supervisor external         → external（探针在场时可达；未注入探针的兼容
//	                              兜底态下原理上不可达）
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

// mapKernelError 还原 snipaste 既有终态文案："已手动停止"抹平为空串（原实现
// 手动退出终态无错误文案）；内核异常退出消息改回"Snipaste 异常退出（退出码
// N）"；其余（进程创建/Job 绑定失败等）透传内核措辞。
func mapKernelError(msg string) string {
	switch {
	case msg == "", msg == "已手动停止":
		return ""
	}
	if code, ok := exitCodeFromKernelMessage(msg); ok {
		return fmt.Sprintf("Snipaste 异常退出（退出码 %d）", code)
	}
	return msg
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

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}
