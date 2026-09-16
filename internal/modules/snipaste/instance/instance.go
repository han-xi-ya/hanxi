// Package instance 实现 Snipaste 自有实例的进程生命周期托管：
// 启动建身份 token → JobObject 绑定（解除退出联动）→ 分层退出（WM_CLOSE→宽限→强杀）。
// 依赖方向：仅依赖 internal/platform 抽象，事件回调解耦由 Callbacks 注入，不引用 service/wails。
package instance

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
)

// State 引擎状态机：stopped → starting → running ⇄ quitting → (stopped | failed)。
type State string

const (
	StateStopped  State = "stopped"  // 未运行或已正常退出
	StateStarting State = "starting" // 进程创建与 Job 绑定窗口
	StateRunning  State = "running"  // 自有托管实例运行中
	StateQuitting State = "quitting" // 已发关闭请求、等待退出（宽限期内）
	StateFailed   State = "failed"   // 启动失败或异常退出
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限；包级变量供单测压缩，生产 2.5s。
var closeGracePeriod = 2500 * time.Millisecond

// Snapshot 引擎状态快照，事件推送与前端渲染共用；仅本引擎自有实例，无 external 态。
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

// Engine Snipaste 自有实例运行引擎（不嗅探外部实例，管理边界=本会话启动的进程树）。
// opMu 串行化 Start/Quit 整段操作；mu 仅保护字段读写。
// generation 为启动代际号：wait goroutine 结束旧进程时若代际已推进，则跳过状态回写（防止误覆盖新一轮状态）。
type Engine struct {
	opMu sync.Mutex
	mu   sync.Mutex

	state      State
	version    string
	pid        uint32
	exePath    string
	exitCode   int
	errMsg     string
	startedAt  time.Time
	stoppedAt  time.Time
	stopping   bool
	generation uint64

	cmd      *exec.Cmd
	job      platform.Job
	token    platform.VerifyToken
	waitDone chan struct{}

	jobAPI     platform.JobAPI
	processAPI platform.ProcessAPI
	closeByPID func(uint32) int
	cb         Callbacks
}

// NewEngine 创建引擎（初始 stopped）。closeByPID 固定用 Win32 WM_CLOSE 投递实现，单测可换替身。
func NewEngine(jobAPI platform.JobAPI, processAPI platform.ProcessAPI, cb Callbacks) *Engine {
	return &Engine{
		state: StateStopped, jobAPI: jobAPI, processAPI: processAPI,
		closeByPID: postCloseByPID, cb: cb,
	}
}

// Start 冷启动自有实例：进程创建 → Query 建立身份 token 并核对 exe 路径（防启动器转发到意外进程）
// → 挂入 JobObject 后立即解除 KILL_ON_JOB_CLOSE（Snipaste 设计上跨 Hanxi 生命周期存活，
// 页面手动 Quit 才负责收尾）。任一环节失败即杀进程并回收句柄，状态落 failed。
// 已在运行/启动中/退出中时拒绝重复启动；wait goroutine 随本方法成功而启动。
func (e *Engine) Start(opts StartOptions) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if opts.Exe == "" {
		return fmt.Errorf("Snipaste.exe 路径不能为空")
	}
	e.mu.Lock()
	if e.state == StateRunning || e.state == StateStarting || e.state == StateQuitting {
		e.mu.Unlock()
		return fmt.Errorf("本会话启动的 Snipaste 已在运行")
	}
	e.generation++
	gen := e.generation
	e.version, e.exePath, e.stopping = opts.Version, opts.Exe, false
	e.errMsg, e.exitCode = "", 0
	e.mu.Unlock()
	e.transition(StateStarting, "")

	cmd := exec.Command(opts.Exe)
	cmd.Dir = filepath.Dir(opts.Exe)
	if err := cmd.Start(); err != nil {
		e.transition(StateFailed, "进程启动失败: "+err.Error())
		return err
	}
	pid := uint32(cmd.Process.Pid)
	info, err := e.processAPI.Query(pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		e.transition(StateFailed, "建立进程身份失败: "+err.Error())
		return fmt.Errorf("建立进程身份失败: %w", err)
	}
	if info.ExePath != "" && !strings.EqualFold(filepath.Clean(info.ExePath), filepath.Clean(opts.Exe)) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		err := fmt.Errorf("启动进程路径不匹配: %s", info.ExePath)
		e.transition(StateFailed, err.Error())
		return err
	}

	job, err := e.jobAPI.Create()
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		e.transition(StateFailed, "创建 Job Object 失败: "+err.Error())
		return err
	}
	if err := job.Assign(pid); err != nil {
		_ = job.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		e.transition(StateFailed, "JobObject 绑定失败: "+err.Error())
		return err
	}
	if err := job.SetAllowKillOnClose(false); err != nil {
		_ = job.Terminate(1)
		_ = job.Close()
		_ = cmd.Wait()
		e.transition(StateFailed, "解除 Hanxi 退出联动失败: "+err.Error())
		return err
	}
	// 关闭 KILL_ON_JOB_CLOSE 后，即使 Hanxi 退出时丢失 Job 句柄，Snipaste 也会继续运行。
	// 页面手动 Quit 仍可在本会话存活期间通过该 Job 精确终止自有进程树。

	waitDone := make(chan struct{})
	e.mu.Lock()
	e.cmd, e.job, e.pid = cmd, job, pid
	e.token = platform.VerifyToken{PID: pid, ExePath: info.ExePath, StartedAt: info.StartedAt}
	e.startedAt, e.stoppedAt, e.waitDone = info.StartedAt, time.Time{}, waitDone
	if e.startedAt.IsZero() {
		e.startedAt = time.Now()
		e.token.StartedAt = e.startedAt
	}
	e.mu.Unlock()
	go e.wait(cmd, job, waitDone, gen)
	e.transition(StateRunning, "")
	return nil
}

// Quit 分层退出：投递 WM_CLOSE → 等待宽限期 → Job 强杀 → KillVerified 兜底。
// 每次动手前 verifyToken 复核 PID 身份（路径+启动时间），身份不匹配立即拒绝并报错——
// 宁可退出失败也不误杀复用同一 PID 的其他进程。非托管状态返回 Method="not-managed" 不视为错误。
func (e *Engine) Quit() (QuitResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	e.mu.Lock()
	if e.state != StateRunning && e.state != StateStarting {
		e.mu.Unlock()
		return QuitResult{Method: "not-managed"}, nil
	}
	token, pid, job, waitDone := e.token, e.pid, e.job, e.waitDone
	e.stopping = true
	e.mu.Unlock()

	if err := e.verifyToken(token); err != nil {
		if err == platform.ErrProcessNotFound {
			return QuitResult{Stopped: true, Method: "already-exited"}, nil
		}
		return QuitResult{Method: "ownership-lost"}, fmt.Errorf("进程身份复核失败，已拒绝退出以避免误杀: %w", err)
	}
	e.transition(StateQuitting, "")
	requested := e.closeByPID(pid) > 0
	select {
	case <-waitDone:
		return QuitResult{Stopped: true, CloseRequested: requested, Method: "close-request"}, nil
	case <-time.After(closeGracePeriod):
	}
	if err := e.verifyToken(token); err != nil {
		if err == platform.ErrProcessNotFound {
			return QuitResult{Stopped: true, CloseRequested: requested, Method: "already-exited"}, nil
		}
		return QuitResult{CloseRequested: requested, Method: "ownership-lost"}, fmt.Errorf("强制结束前进程身份已变化，已拒绝操作: %w", err)
	}
	if job != nil {
		if err := job.Terminate(1); err == nil {
			return QuitResult{Stopped: true, Forced: true, CloseRequested: requested, Method: "forced-job"}, nil
		}
	}
	if err := e.processAPI.KillVerified(context.Background(), token, true); err != nil {
		return QuitResult{CloseRequested: requested, Method: "forced-process"}, err
	}
	return QuitResult{Stopped: true, Forced: true, CloseRequested: requested, Method: "forced-process"}, nil
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

// wait 后台 goroutine（Start 派生）：阻塞至进程退出，关闭 Job 句柄并回写终态。
// gen 不匹配说明已有新一轮 Start 接管状态字段，仅关闭 done 后静默退出，绝不回写。
// stopping 或退出码 0 归为正常 stopped，其余非零码落 failed 供前端提示。
func (e *Engine) wait(cmd *exec.Cmd, job platform.Job, done chan struct{}, gen uint64) {
	err := cmd.Wait()
	code := 0
	if err != nil && cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	_ = job.Close()

	e.mu.Lock()
	if gen != e.generation {
		e.mu.Unlock()
		close(done)
		return
	}
	stopping := e.stopping
	e.exitCode, e.stoppedAt = code, time.Now()
	e.cmd, e.job, e.pid, e.waitDone = nil, nil, 0, nil
	e.token = platform.VerifyToken{}
	e.mu.Unlock()
	close(done)
	if stopping || code == 0 {
		e.transition(StateStopped, "")
	} else {
		e.transition(StateFailed, fmt.Sprintf("Snipaste 异常退出（退出码 %d）", code))
	}
}

// Snapshot 返回当前状态的一致性拷贝（字段级快照，不保证跨字段时序）。
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *Engine) transition(state State, message string) {
	e.mu.Lock()
	e.state, e.errMsg = state, message
	snap := e.snapshotLocked()
	e.mu.Unlock()
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

func (e *Engine) snapshotLocked() Snapshot {
	return Snapshot{Version: e.version, State: e.state, PID: e.pid, ExePath: e.exePath, ExitCode: e.exitCode, Error: e.errMsg, StartedAt: e.startedAt, StoppedAt: e.stoppedAt}
}
