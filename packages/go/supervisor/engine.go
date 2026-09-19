package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/platform"
)

// 时长参数包级变量：单测压缩等待窗口（蓝本同款手法），生产值见注释。
var (
	// readyPollInterval 就绪轮询间隔（蓝本 ddnsgo/ocr 同值 150ms）。
	readyPollInterval = 150 * time.Millisecond
	// settleTimeout 终止动作后等待 wait 协程收口（终态回写完成）的上限
	//（蓝本 quitSettleWait/quitGrace 为 3s，frpc Stop 为 10s，取中值）。
	settleTimeout = 5 * time.Second
	// inspectTimeout 单次探针调用的引擎侧兜底超时。
	inspectTimeout = 5 * time.Second
)

// logLineMaxBytes 单行日志上限（64KB，与 envcheck/npmtool 流式读取同口径）：
// 超长行使该路泵以 ErrTooLong 收口并转为一条可见日志，不无界占用内存。
const logLineMaxBytes = 64 << 10

// readyWait 循环的终止原因（awaitReady 内部区分，Start 映射为文案）。
var (
	errReadyTimeout = errors.New("supervisor: readiness timeout")
	errReadyDead    = errors.New("supervisor: process exited before ready")
)

// Engine 单模块实例引擎。并发纪律：mu 保护状态字段，startMu 串行化 Start/Stop
// 整段操作，cmd.Wait 由唯一 wait 协程持有；上一代未收口时 Start 返回 ErrBusy，
// 因此任一时刻至多存在一个在途 wait 协程，无需蓝本 frpc/snipaste 的代际号防误写。
type Engine struct {
	mu sync.Mutex

	state    State
	version  string
	pid      uint32
	exe      string
	errMsg   string
	since    time.Time
	stopping bool // 手动停止/终止标记：防止进程终止后误判为异常退出

	startMu sync.Mutex
	h       processHandle         // 当前受管进程（收口后置 nil）
	done    chan struct{}         // wait 协程完成终态回写后关闭；nil=无在途进程
	job     platform.Job          // 当前 Job Object 句柄
	token   *platform.VerifyToken // 启动时建立的身份令牌（注入 ProcessAPI 后）

	jobs     platform.JobAPI
	probe    Probe
	procAPI  platform.ProcessAPI
	open     opener
	cb       Callbacks
	quitHook func(ctx context.Context) error

	// 日志泵协程计数（跨代累计）：拉起即计 started，管道 EOF/出错退出计 finished。
	// 泵随管道收口自行终止、不并入 wait 收口链路（日志通道不得阻塞进程治理），
	// 计数供测试断言"进程退出后 goroutine 不泄露"。
	pumpStarted  atomic.Int64
	pumpFinished atomic.Int64
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobs platform.JobAPI, probe Probe, cb Callbacks) *Engine {
	return &Engine{
		state: StateStopped,
		jobs:  jobs,
		probe: probe,
		open:  defaultOpener,
		cb:    cb,
	}
}

// WithProcessAPI 注入进程管理 API（可选）：启用后 Start 建立 VerifyToken 身份、
// Stop 的兜底强杀路径改走 KillVerified 复核，防 PID 快速复用误杀。链式调用。
func (e *Engine) WithProcessAPI(p platform.ProcessAPI) *Engine {
	e.procAPI = p
	return e
}

// SetQuitHook 注册命令通道优雅退出钩子（管道/WM_CLOSE/HTTP shutdown 等由模块注入）。
// Stop(grace) 在 grace 窗口内先行调用；钩子为 nil 或返回错误时直接进入强制终止。
func (e *Engine) SetQuitHook(h func(ctx context.Context) error) {
	e.mu.Lock()
	e.quitHook = h
	e.mu.Unlock()
}

// Start 启动自有实例：spawn → Job Create/Assign →（可选）就绪探测 → 分类入账。
//
// 流程与蓝本对齐：
//   - ReadyTimeout==0：绑定成功即升 running（markeron 冷启动语义，竞速误判交给
//     wait() 的 external-takeover 分类兜底）；
//   - ReadyTimeout>0：就绪前停留在 starting（ddnsgo/ocr 语义——"进程活着"不作
//     成功信号）；就绪失败/进程中途退出则主动终止并落 failed，不空等满超时。
//
// 上一个受管进程未收口时返回 ErrBusy（不隐式重启、不并发双 Wait）。
func (e *Engine) Start(ctx context.Context, spec Spec) error {
	if spec.Exe == "" {
		return errors.New("supervisor: Spec.Exe 不能为空")
	}
	if spec.WorkingDir == "" {
		spec.WorkingDir = filepath.Dir(spec.Exe) // 工作目录锁定，蓝本惯例
	}

	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	if e.done != nil {
		e.mu.Unlock()
		return ErrBusy
	}
	e.version = spec.Version
	e.errMsg = ""
	e.stopping = false
	e.mu.Unlock()

	e.transition(StateStarting, "")

	h, err := e.open(spec)
	if err != nil {
		e.transition(StateFailed, "进程创建失败: "+err.Error())
		return fmt.Errorf("supervisor: 进程创建失败: %w", err)
	}
	// 输出管道须在 Start 前获取（exec.Cmd 语义）；泵协程延后到 Start 成功后拉起，
	// 避免启动失败路径去读已废弃管道、刷出无主错误日志。
	var pumpReaders []io.Reader
	if e.cb.OnLog != nil {
		if r, perr := h.StdoutPipe(); perr == nil && r != nil {
			pumpReaders = append(pumpReaders, r)
		}
		if r, perr := h.StderrPipe(); perr == nil && r != nil {
			pumpReaders = append(pumpReaders, r)
		}
	}
	if err := h.Start(); err != nil {
		e.transition(StateFailed, "进程启动失败: "+err.Error())
		return fmt.Errorf("进程启动失败: %w", err)
	}
	for _, r := range pumpReaders {
		e.startPump(r)
	}

	// 登记即派生监督协程：后续任何失败路径都可安全地"标记 stopping → 终止 →
	// 等收口 → 覆盖终态"，wait 的所有者自始至终唯一。
	done := make(chan struct{})
	e.mu.Lock()
	e.h = h
	e.done = done
	e.pid = h.PID()
	e.exe = spec.Exe
	e.since = time.Now()
	e.captureTokenLocked(h.PID())
	e.mu.Unlock()
	go e.wait(h, done)

	job, jerr := e.jobs.Create()
	if jerr != nil {
		return e.abortStart(done, fmt.Sprintf("创建 Job Object 失败: %v", jerr), jerr)
	}
	if aerr := job.Assign(e.currentPID()); aerr != nil {
		_ = job.Close()
		return e.abortStart(done, fmt.Sprintf("JobObject 绑定失败: %v", aerr), aerr)
	}
	if spec.DetachFromJob {
		// 解除退出联动：Hanxi 退出/崩溃不再连带杀本实例（"不随 Hanxi 关闭"开关）
		if derr := job.SetAllowKillOnClose(false); derr != nil {
			_ = job.Close()
			return e.abortStart(done, fmt.Sprintf("解除退出联动失败: %v", derr), derr)
		}
	}
	e.mu.Lock()
	e.job = job
	e.mu.Unlock()

	if spec.ReadyTimeout <= 0 {
		e.transition(StateRunning, "")
		return nil
	}

	ready, info, rerr := e.awaitReady(ctx, done, spec.ReadyTimeout)
	if !ready {
		msg := fmt.Sprintf("启动后实例始终未就绪（%v），已终止", rerr)
		if errors.Is(rerr, context.DeadlineExceeded) || errors.Is(rerr, errReadyTimeout) {
			msg = fmt.Sprintf("启动后实例在 %v 内未就绪，已终止", spec.ReadyTimeout)
		} else if errors.Is(rerr, context.Canceled) {
			msg = "就绪等待被取消，已终止"
		}
		return e.abortStart(done, msg, rerr)
	}
	// 就绪判定的竞态复核：探针说"在运行"，但自有进程可能已退出——
	// 典型场景是单实例工具的冷启动竞速（我方是转发让位的第二实例），
	// 此时终态由 wait() 的分类（external/stopped/failed）决定，Start 不覆盖。
	if settled(done) {
		final := e.Snapshot()
		if final.State == StateExternal {
			return fmt.Errorf("supervisor: 自有进程退出后由外部实例接管: %w", ErrExternal)
		}
		return errors.New("supervisor: 进程在就绪判定完成前已退出")
	}
	if info != nil {
		e.mu.Lock()
		if e.h == h { // 进程仍在世才采纳探针细节
			if info.PID != 0 {
				e.pid = info.PID
			}
			if info.ExePath != "" {
				e.exe = info.ExePath
			}
			if !info.StartedAt.IsZero() {
				e.since = info.StartedAt
			}
		}
		e.mu.Unlock()
	}
	e.transition(StateRunning, "")
	return nil
}

// Stop 停止实例：先以探针判定归属。
//   - 受管（自家进程在世）：置终止标记 →（注册过 QuitHook 且 grace>0 时）优雅退出
//     并等待 grace 窗口 → job.Terminate →（失败时）KillVerified/h.Kill 兜底 →
//     等 wait 协程收口；
//   - 外部：不强杀，校正状态为 external 并返回 ErrExternal（调用方给出指引）；
//   - 无：幂等返回 nil。
func (e *Engine) Stop(grace time.Duration) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.mu.Lock()
	h, done, hook := e.h, e.done, e.quitHook
	e.mu.Unlock()

	if h == nil {
		own, info, perr := e.classify(context.Background())
		switch own {
		case OwnExternal:
			e.applyExternal(info)
			return ErrExternal
		case OwnManaged:
			// classify 与上方快照间的窄竞态：按受管路径继续（h/done 已不可得则退化为校正）。
			e.mu.Lock()
			h, done = e.h, e.done
			e.mu.Unlock()
			if h == nil {
				return nil
			}
		default: // OwnNone（含探针失败）：静止态幂等；确认消失才校正过期的 external 记录
			if perr == nil {
				e.applyStopped()
			}
			return nil
		}
	}

	e.mu.Lock()
	e.stopping = true // 先标记：其后 wait() 无论何因收尾都归类为"手动停止"
	e.mu.Unlock()
	e.transition(StateStopping, "")

	// 优雅通道：钩子成功投递才消耗 grace 窗口等待自然退出；否则立即强制。
	if hook != nil && grace > 0 && done != nil {
		hctx, cancel := context.WithTimeout(context.Background(), grace)
		herr := hook(hctx)
		cancel()
		if herr == nil && waitClosed(done, grace) {
			return nil
		}
	}

	kerr := e.killSequence(context.Background())
	if done != nil && !waitClosed(done, settleTimeout) {
		return errors.Join(kerr, errors.New("supervisor: 等待进程收尾超时"))
	}
	if kerr != nil {
		return fmt.Errorf("supervisor: 终止托管进程失败: %w", kerr)
	}
	return nil
}

// Snapshot 返回当前状态的一致性快照。
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// Ownership 报告当前归属判定：自有进程在世即 OwnManaged，否则以探针裁决；
// 探针失败保守判 OwnNone。
func (e *Engine) Ownership(ctx context.Context) Ownership {
	own, _, _ := e.classify(ctx)
	return own
}

// RefreshExternal 探针校正 external/stopped 静止态（轮询/间隙补偿入口）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.mu.Lock()
	state := e.state
	e.mu.Unlock()
	if state == StateRunning || state == StateStarting || state == StateStopping {
		return
	}

	own, info, err := e.classify(context.Background())
	if err != nil {
		return // 探针瞬时失败：不改变状态（间隙补偿下一轮再来）
	}
	switch own {
	case OwnExternal:
		e.applyExternal(info)
	case OwnNone:
		e.applyStopped() // 探针确认目标已消失，撤销过期的 external 记录
	default: // OwnManaged 不应出现（h==nil 前提）
	}
}

// ---------- 内部实现 ----------

// classify 归属判定：受管优先信任自家句柄，外部以探针为准；探针失败保守判 OwnNone。
func (e *Engine) classify(ctx context.Context) (Ownership, *platform.ProcInfo, error) {
	e.mu.Lock()
	h := e.h
	e.mu.Unlock()
	if h != nil {
		return OwnManaged, nil, nil
	}
	ictx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	running, info, err := e.probe.Inspect(ictx)
	if err != nil {
		return OwnNone, nil, err
	}
	if !running {
		return OwnNone, nil, nil
	}
	return OwnExternal, info, nil
}

// captureTokenLocked 启动时建立 VerifyToken 身份（需持 mu；未注入 ProcessAPI 或
// 查询失败时为 nil——兜底强杀退化为句柄 Kill，Job Terminate 不受影响）。
func (e *Engine) captureTokenLocked(pid uint32) {
	e.token = nil
	if e.procAPI == nil {
		return
	}
	info, err := e.procAPI.Query(pid)
	if err != nil {
		return
	}
	e.token = &platform.VerifyToken{PID: pid, ExePath: info.ExePath, StartedAt: info.StartedAt}
	if !info.StartedAt.IsZero() {
		e.since = info.StartedAt
	}
}

// awaitReady 轮询探针直至就绪/进程退出/超时/取消。进程退出与就绪可能同帧发生，
// 检测到退出后仍做末位复探（蓝本 waitPortReady/waitReady 同款手法）。
// 返回的 info 仅在 ready==true 时可能非 nil。
func (e *Engine) awaitReady(ctx context.Context, done chan struct{}, timeout time.Duration) (bool, *platform.ProcInfo, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ictx, cancel := context.WithTimeout(ctx, inspectTimeout)
		running, info, err := e.probe.Inspect(ictx)
		cancel()
		if err != nil {
			lastErr = err
		} else if running {
			return true, info, nil
		}
		if settled(done) {
			ictx2, cancel2 := context.WithTimeout(ctx, inspectTimeout)
			r2, info2, err2 := e.probe.Inspect(ictx2)
			cancel2()
			if err2 == nil && r2 {
				return true, info2, errReadyDead // 调用方经 done 复核判定接管
			}
			return false, nil, errReadyDead
		}
		if ctx.Err() != nil {
			return false, nil, ctx.Err()
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			if lastErr != nil {
				return false, nil, lastErr
			}
			return false, nil, errReadyTimeout
		}
		sleep := readyPollInterval
		if remain < sleep {
			sleep = remain
		}
		time.Sleep(sleep)
	}
}

// abortStart Start 失败路径的统一收口：标记 stopping → 终止 → 等 wait 收口 →
// 覆盖为 failed + 原因文案（wait 协程因 stopping 会先落"已手动停止"，随后被
// 这里的 failed 覆盖，事件序列与蓝本 ddnsgo 就绪失败路径一致）。
func (e *Engine) abortStart(done chan struct{}, msg string, cause error) error {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()
	_ = e.killSequence(context.Background())
	if done != nil {
		waitClosed(done, settleTimeout)
	}
	e.transition(StateFailed, msg)
	return fmt.Errorf("%s（%v）", msg, cause)
}

// killSequence 强制终止序列：job.Terminate 优先（整树语义、不受 PID 复用影响），
// 失败/未绑定时兜底强杀——注入 ProcessAPI 且令牌在场时走 KillVerified 复核，
// 身份不符（PID 复用/已消失）宁可拒杀报错也不误伤陌生进程。
func (e *Engine) killSequence(ctx context.Context) error {
	e.mu.Lock()
	job, h, token := e.job, e.h, e.token
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
		return errors.New("supervisor: 实例没有可终止的进程")
	}
	if e.procAPI != nil && token != nil {
		if err := e.procAPI.KillVerified(ctx, *token, true); err != nil {
			if errors.Is(err, platform.ErrProcessNotFound) {
				return nil // 已消失即达成目标
			}
			return err // 复核不通过（复用/红线/权限）：拒杀并上报
		}
		return nil
	}
	return h.Kill()
}

// startPump 拉起单路日志泵：逐行转发子进程输出（行内容不含换行符）。
// scanner 结束后必查 Err（仓内口径）：管道半路故障、单行超限等转为一条可见
// 日志而非静默吞掉。脱敏/缓冲/限速等策略由模块层在 OnLog 回调内自理（见包注释）。
// 回调在泵协程内直调、不持引擎锁（与 emit 同款纪律，防回调重入死锁）。
func (e *Engine) startPump(r io.Reader) {
	onLog := e.cb.OnLog
	e.pumpStarted.Add(1)
	go func() {
		defer e.pumpFinished.Add(1)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, logLineMaxBytes), logLineMaxBytes)
		for sc.Scan() {
			onLog(sc.Text())
		}
		if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
			onLog(fmt.Sprintf("…托管进程输出读取中断: %v…", err))
		}
	}()
}

// wait 监督协程：阻塞等待自有进程退出并分类收尾（cmd.Wait 唯一所有者）。
// 分类顺序承蓝本纪律且不可调换：
//  1. stopping（手动停止/Start 失败收口）→ stopped，原因文案由上层覆盖；
//  2. 探针仍见目标存活 → 外部接管（冷启动竞速中我方是让位的第二实例）→ external；
//  3. 退出码 0 且无崩溃预期 → stopped；
//  4. 其余（含被第三方 TerminateProcess——非零码且探针未见存活）→ failed。
func (e *Engine) wait(h processHandle, done chan struct{}) {
	err := h.Wait()
	code := 0
	if err != nil {
		code = h.ExitCode()
	}

	e.mu.Lock()
	stopping := e.stopping
	e.mu.Unlock()

	var extInfo *platform.ProcInfo
	externalTaken := false
	if !stopping {
		ictx, cancel := context.WithTimeout(context.Background(), inspectTimeout)
		running, info, perr := e.probe.Inspect(ictx)
		cancel()
		externalTaken = perr == nil && running
		extInfo = info
	}

	e.mu.Lock()
	if e.h == h { // 当前代才回写（ErrBusy 前提下恒成立，保留作纵深防御）
		e.h = nil
		if e.job != nil {
			_ = e.job.Close()
			e.job = nil
		}
		e.token = nil
		e.stopping = false
	}
	e.mu.Unlock()

	switch {
	case stopping:
		e.transition(StateStopped, "已手动停止")
	case externalTaken:
		e.applyExternal(extInfo)
	case code == 0:
		e.transition(StateStopped, "")
	default:
		e.transition(StateFailed, fmt.Sprintf("托管进程异常退出（退出码 %d）", code))
	}

	// done 最后关闭：观察方（Stop/abortStart）据此判定"终态已回写、资源已释放"。
	close(done)
	e.mu.Lock()
	if e.done == done {
		e.done = nil
	}
	e.mu.Unlock()
}

// applyExternal 校正/入账外部实例（探针 ProcInfo 可得时充实快照）。
func (e *Engine) applyExternal(info *platform.ProcInfo) {
	var snap Snapshot
	var changed bool
	e.mu.Lock()
	changed = e.state != StateExternal
	e.state = StateExternal
	e.errMsg = ""
	e.stopping = false
	e.exe = "" // 外部实例路径不可信为托管值
	e.pid = 0
	e.since = time.Time{}
	if info != nil {
		e.pid = info.PID
		e.since = info.StartedAt
		if info.ExePath != "" {
			e.exe = info.ExePath // 探针报出的外部路径是事实信息，可展示
		}
	}
	snap = e.snapshotLocked()
	e.mu.Unlock()
	if changed {
		e.emit(snap) // 无变化不广播
	}
}

// applyStopped 撤销过期的 external 记录（探针确认目标已消失）。
func (e *Engine) applyStopped() {
	var snap Snapshot
	var changed bool
	e.mu.Lock()
	if e.state == StateExternal {
		e.state = StateStopped
		e.errMsg = ""
		e.pid = 0
		e.since = time.Time{}
		changed = true
		snap = e.snapshotLocked()
	}
	e.mu.Unlock()
	if changed {
		e.emit(snap)
	}
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked() Snapshot {
	return Snapshot{
		State:   e.state,
		PID:     e.pid,
		Version: e.version,
		Exe:     e.exe,
		Error:   e.errMsg,
		Managed: e.h != nil && (e.state == StateStarting || e.state == StateRunning || e.state == StateStopping),
		Since:   e.since,
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
	if s == StateRunning || s == StateStarting {
		e.errMsg = ""
	}
	snap = e.snapshotLocked()
	e.mu.Unlock()
	e.emit(snap)
}

func (e *Engine) currentPID() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pid
}

// settled 非阻塞探测 done 是否已关闭（wait 协程完成收口的标志）。
func settled(done chan struct{}) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
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
