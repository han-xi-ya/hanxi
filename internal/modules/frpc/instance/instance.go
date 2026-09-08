// Package instance 实现 frpc 多实例运行引擎（M4.3）：
//
// 每个项目独立启动一个 frpc.exe 子进程，绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE）——Hanxi 无论以何种方式退出，
// 内核都会连带强杀整个 frpc 进程树，杜绝孤儿进程占端口。
//
// 实例进程的 stdout/stderr 汇入内存环形日志（保留最近 1000 行），
// 状态迁移与日志行经回调通知外层（service 层转 wails 事件推送前端）。
// 本包零框架依赖，便于单元测试。
package instance

import (
	"bufio"
	"errors"
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

// logCapacity 内存环形日志容量（行）。
const logCapacity = 1000

// State 实例生命周期状态机：stopped → starting → running → (stopped | failed)
type State string

const (
	StateStopped  State = "stopped"  // 未启动 / 正常退出 / 手动停止
	StateStarting State = "starting" // 进程创建中（极短窗口）
	StateRunning  State = "running"  // 运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
)

// ConnState frpc 细粒度网络连接状态
type ConnState string

const (
	ConnStateIdle         ConnState = "idle"         // 未连接/停止
	ConnStateConnecting   ConnState = "connecting"   // 正在握手连接服务端
	ConnStateConnected    ConnState = "connected"    // 握手成功，穿透正常工作
	ConnStateAuthFailed   ConnState = "auth_failed"  // Token 或认证错误
	ConnStateReconnecting ConnState = "reconnecting" // 网络中断/服务端重连中
	ConnStateError        ConnState = "error"        // 协议或配置严重错误
)

// Snapshot 实例状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	ProjectID   string    `json:"projectId"`
	ProjectName string    `json:"projectName"`
	Version     string    `json:"version"`
	State       State     `json:"state"`
	ConnState   ConnState `json:"connState"`
	PID         uint32    `json:"pid"`
	ExitCode    int       `json:"exitCode"`
	Error       string    `json:"error"`
	StartedAt   time.Time `json:"startedAt"`
	StoppedAt   time.Time `json:"stoppedAt"`
}

// LogEntry 单条实例日志（事件 frpc:instance-log 载荷）。
type LogEntry struct {
	ProjectID string `json:"projectId"`
	Line      string `json:"line"`
}

// StartOptions 启动参数：由 service 层解析项目后填充。
type StartOptions struct {
	ProjectID   string   // 项目 ID（实例唯一键）
	ProjectName string   // 项目名（展示用）
	Version     string   // 绑定版本 vX.Y.Z
	FrpcExe     string   // frpc.exe 绝对路径（版本隔离目录内）
	ConfigPath  string   // 已落盘的 frpc TOML 配置路径
	Redact      []string // 需在日志中置换的敏感串（如 auth token）
}

func (o StartOptions) validate() error {
	switch {
	case o.ProjectID == "":
		return fmt.Errorf("项目 ID 不能为空")
	case o.ProjectName == "":
		return fmt.Errorf("项目名称不能为空")
	case o.FrpcExe == "":
		return fmt.Errorf("frpc.exe 路径不能为空")
	case o.ConfigPath == "":
		return fmt.Errorf("配置文件路径不能为空")
	}
	return nil
}

// Callbacks 实例事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
	OnLog   func(projectID string, line string)
}

type processRun struct {
	cmd      *exec.Cmd
	job      platform.Job
	done     chan struct{}
	redact   []string
	stopping bool
}

// Instance 单个项目的运行实例。
type Instance struct {
	projectID   string
	projectName string

	mu        sync.Mutex
	state     State
	connState ConnState
	version   string
	pid       uint32
	exitCode  int
	errMsg    string
	startedAt time.Time
	stoppedAt time.Time
	stopping  bool // 手动停止标记：防止 kill 后误判为异常退出

	startMu sync.Mutex // Start/Stop 互斥临界区
	run     *processRun
	logs    *ringbuf.RingBuffer
	cb      Callbacks
	jobAPI  platform.JobAPI
}

// newInstance 创建实例（初始状态 stopped）。
func newInstance(opts StartOptions, jobAPI platform.JobAPI, cb Callbacks) *Instance {
	return &Instance{
		projectID:   opts.ProjectID,
		projectName: opts.ProjectName,
		version:     opts.Version,
		state:       StateStopped,
		connState:   ConnStateIdle,
		logs:        ringbuf.New(logCapacity),
		cb:          cb,
		jobAPI:      jobAPI,
	}
}

// Start 启动实例；已运行或仍在回收时幂等返回，不隐式重启。
func (in *Instance) Start(opts StartOptions) error {
	in.startMu.Lock()
	defer in.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}
	in.mu.Lock()
	if in.run != nil {
		in.mu.Unlock()
		return nil
	}
	in.version = opts.Version
	in.mu.Unlock()
	in.transition(StateStarting, ConnStateConnecting, "")

	cmd := exec.Command(opts.FrpcExe, "-c", opts.ConfigPath)
	cmd.Dir = filepath.Dir(opts.FrpcExe)
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		in.transition(StateFailed, ConnStateError, "打开进程输出管道失败: "+err.Error())
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		in.transition(StateFailed, ConnStateError, "打开进程错误管道失败: "+err.Error())
		return err
	}
	if err := cmd.Start(); err != nil {
		in.transition(StateFailed, ConnStateError, "进程启动失败: "+err.Error())
		return err
	}

	run := &processRun{cmd: cmd, done: make(chan struct{}), redact: append([]string(nil), opts.Redact...)}
	in.mu.Lock()
	in.run = run
	in.pid = uint32(cmd.Process.Pid)
	in.exitCode = 0
	in.errMsg = ""
	in.startedAt = time.Now()
	in.stoppedAt = time.Time{}
	in.mu.Unlock()

	job, jerr := in.jobAPI.Create()
	if jerr != nil {
		_ = cmd.Process.Kill()
		go in.wait(run)
		in.transitionIfCurrent(run, StateFailed, ConnStateError, "创建 Job Object 失败: "+jerr.Error())
		return fmt.Errorf("创建 Job Object 失败: %w", jerr)
	}
	if aerr := job.Assign(in.pid); aerr != nil {
		_ = job.Close()
		_ = cmd.Process.Kill()
		go in.wait(run)
		in.transitionIfCurrent(run, StateFailed, ConnStateError, "JobObject 绑定失败: "+aerr.Error())
		return fmt.Errorf("JobObject 绑定失败: %w", aerr)
	}
	run.job = job

	// 先登记 running，再让极快退出的 wait 有机会落最终状态。
	in.transitionIfCurrent(run, StateRunning, ConnStateConnecting, "")
	go in.pump(run, stdout)
	go in.pump(run, stderr)
	go in.wait(run)
	return nil
}

const stopWaitTimeout = 10 * time.Second

// Stop 停止实例并等待本代回收完成。
func (in *Instance) Stop() error {
	in.startMu.Lock()
	defer in.startMu.Unlock()

	in.mu.Lock()
	run := in.run
	if run == nil {
		in.mu.Unlock()
		return nil
	}
	run.stopping = true
	cmd, job, done := run.cmd, run.job, run.done
	in.mu.Unlock()

	var stopErr error
	if job != nil {
		stopErr = job.Terminate(1)
	}
	if stopErr != nil || job == nil {
		if cmd != nil && cmd.Process != nil {
			stopErr = cmd.Process.Kill()
		} else if stopErr == nil {
			stopErr = fmt.Errorf("实例没有可终止的进程（可能仍在启动）")
		}
	}
	select {
	case <-done:
		return stopErr
	case <-time.After(stopWaitTimeout):
		if stopErr != nil {
			return errors.Join(stopErr, fmt.Errorf("等待 frpc 进程退出超时"))
		}
		return fmt.Errorf("等待 frpc 进程退出超时")
	}
}

// Snapshot 返回当前状态快照。
func (in *Instance) Snapshot() Snapshot {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.snapshotLocked()
}

// Logs 返回最近 n 行日志。
func (in *Instance) Logs(n int) []string {
	return in.logs.Last(n)
}

// RunningFrom 已运行时长
func (in *Instance) RunningDuration() time.Duration {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.state != StateRunning || in.startedAt.IsZero() {
		return 0
	}
	return time.Since(in.startedAt)
}

// snapshotLocked 前置条件：已持 in.mu。
func (in *Instance) snapshotLocked() Snapshot {
	return Snapshot{
		ProjectID:   in.projectID,
		ProjectName: in.projectName,
		Version:     in.version,
		State:       in.state,
		ConnState:   in.connState,
		PID:         in.pid,
		ExitCode:    in.exitCode,
		Error:       in.errMsg,
		StartedAt:   in.startedAt,
		StoppedAt:   in.stoppedAt,
	}
}

// transition 切换状态并通知回调
func (in *Instance) transition(s State, cs ConnState, errMsg string) {
	var snap Snapshot
	in.mu.Lock()
	in.state = s
	in.connState = cs
	in.errMsg = errMsg
	if s == StateRunning {
		in.stoppedAt = time.Time{}
	} else if s == StateFailed || s == StateStopped {
		if in.stoppedAt.IsZero() {
			in.stoppedAt = time.Now()
		}
	}
	snap = in.snapshotLocked()
	in.mu.Unlock()

	if in.cb.OnState != nil {
		in.cb.OnState(snap)
	}
}

func (in *Instance) transitionIfCurrent(run *processRun, s State, cs ConnState, errMsg string) {
	in.mu.Lock()
	if in.run != run {
		in.mu.Unlock()
		return
	}
	in.state = s
	in.connState = cs
	in.errMsg = errMsg
	if s == StateRunning {
		in.stoppedAt = time.Time{}
	} else if (s == StateFailed || s == StateStopped) && in.stoppedAt.IsZero() {
		in.stoppedAt = time.Now()
	}
	snap := in.snapshotLocked()
	in.mu.Unlock()
	if in.cb.OnState != nil {
		in.cb.OnState(snap)
	}
}

// updateConnState 仅允许当前运行代更新连接状态并广播
func (in *Instance) updateConnState(run *processRun, cs ConnState) {
	var snap Snapshot
	in.mu.Lock()
	if in.run != run || in.connState == cs || in.state != StateRunning {
		in.mu.Unlock()
		return
	}
	in.connState = cs
	snap = in.snapshotLocked()
	in.mu.Unlock()

	if in.cb.OnState != nil {
		in.cb.OnState(snap)
	}
}

// wait 阻塞等待本代进程退出，仅当前代可以写公共状态。
func (in *Instance) wait(run *processRun) {
	err := run.cmd.Wait()
	code := 0
	if err != nil && run.cmd.ProcessState != nil {
		code = run.cmd.ProcessState.ExitCode()
	}
	if run.job != nil {
		_ = run.job.Close()
	}

	in.mu.Lock()
	if in.run != run {
		in.mu.Unlock()
		close(run.done)
		return
	}
	in.exitCode = code
	in.stoppedAt = time.Now()
	stopped := run.stopping
	prev := in.state
	in.run = nil
	in.pid = 0
	if stopped {
		in.state, in.connState, in.errMsg = StateStopped, ConnStateIdle, "已手动停止"
	} else if code == 0 && prev == StateRunning {
		in.state, in.connState, in.errMsg = StateStopped, ConnStateIdle, ""
	} else if prev != StateFailed {
		in.state, in.connState, in.errMsg = StateFailed, ConnStateError, fmt.Sprintf("frpc 进程异常退出（退出码 %d）", code)
	}
	snap := in.snapshotLocked()
	in.mu.Unlock()
	close(run.done)
	if in.cb.OnState != nil {
		in.cb.OnState(snap)
	}
}

// pump 逐行搬运本代子进程输出到环形日志。
func (in *Instance) pump(run *processRun, r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			in.inspectAndWriteLog(run, line)
		}
		if err != nil {
			return
		}
	}
}

// inspectAndWriteLog 统一入口：连接状态关键词嗅探 → 脱敏 → 环形缓冲 → 回调。
func (in *Instance) inspectAndWriteLog(run *processRun, line string) {
	in.mu.Lock()
	current := in.run == run
	in.mu.Unlock()
	if !current {
		return
	}
	lower := strings.ToLower(line)

	if strings.Contains(lower, "login to server success") ||
		strings.Contains(lower, "start proxy success") ||
		strings.Contains(lower, "work connection success") {
		in.updateConnState(run, ConnStateConnected)
	} else if strings.Contains(lower, "authorization failed") ||
		strings.Contains(lower, "token is not correct") ||
		strings.Contains(lower, "token is empty") ||
		strings.Contains(lower, "user or token not matched") {
		in.updateConnState(run, ConnStateAuthFailed)
	} else if strings.Contains(lower, "connect to server error") ||
		strings.Contains(lower, "try to reconnect") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "wait until next retry") {
		in.updateConnState(run, ConnStateReconnecting)
	}

	line = redactText(line, run.redact)
	in.logs.Write(line)
	if in.cb.OnLog != nil {
		in.cb.OnLog(in.projectID, line)
	}
}

// redactText 将秘密串替换为 ***
func redactText(line string, secrets []string) string {
	for _, s := range secrets {
		if s != "" && strings.Contains(line, s) {
			line = strings.ReplaceAll(line, s, "***")
		}
	}
	return line
}
