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

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateQuitting State = "quitting"
	StateFailed   State = "failed"
)

var closeGracePeriod = 2500 * time.Millisecond

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

type StartOptions struct {
	Version string
	Exe     string
}

type Callbacks struct {
	OnState func(Snapshot)
}

type QuitResult struct {
	Stopped        bool
	Forced         bool
	CloseRequested bool
	Method         string
}

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

func NewEngine(jobAPI platform.JobAPI, processAPI platform.ProcessAPI, cb Callbacks) *Engine {
	return &Engine{
		state: StateStopped, jobAPI: jobAPI, processAPI: processAPI,
		closeByPID: postCloseByPID, cb: cb,
	}
}

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
