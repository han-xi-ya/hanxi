// Package instance 实现 frpc 多实例运行引擎（M4.3）：
//
// 每个项目独立启动一个 frpc.exe 子进程，绑定 Windows Job Object
// （JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE）——HubKit 无论以何种方式退出，
// 内核都会连带强杀整个 frpc 进程树，杜绝孤儿进程占端口。
//
// 实例进程的 stdout/stderr 汇入内存环形日志（保留最近 1000 行），
// 状态迁移与日志行经回调通知外层（service 层转 wails 事件推送前端）。
// 本包零框架依赖，便于单元测试。
package instance

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hubkit/internal/platform"
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

// Snapshot 实例状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	ProjectID   string    `json:"projectId"`
	ProjectName string    `json:"projectName"`
	Version     string    `json:"version"`
	State       State     `json:"state"`
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

// Instance 单个项目的运行实例。
// 字段访问：state/pid/exitCode 等状态字段受 mu 保护；
// cmd/job/stopping 由 Start/Stop 临界区（startMu）串行维护，wait goroutine 只读。
type Instance struct {
	projectID   string
	projectName string

	mu      sync.Mutex
	state   State
	version string
	pid     uint32
	exitCode int
	errMsg   string
	startedAt time.Time
	stoppedAt time.Time
	stopping bool // 手动停止标记：防止 kill 后误判为异常退出

	startMu sync.Mutex // Start/Stop 互斥临界区
	cmd     *exec.Cmd
	job     platform.Job
	redact  []string
	logs    *RingBuffer
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
		redact:      append([]string(nil), opts.Redact...),
		logs:        newRingBuffer(logCapacity),
		cb:          cb,
		jobAPI:      jobAPI,
	}
}

// Start 启动/重启实例：创建子进程 → 绑定 JobObject → 状态 running。
// 任一步失败即回滚：杀掉已启动进程并转 failed，不留孤儿。
func (in *Instance) Start(opts StartOptions) error {
	in.startMu.Lock()
	defer in.startMu.Unlock()

	if err := opts.validate(); err != nil {
		return err
	}

	in.mu.Lock()
	in.version = opts.Version
	in.redact = append([]string(nil), opts.Redact...)
	in.stopping = false
	in.mu.Unlock()

	in.transition(StateStarting, "")

	cmd := exec.Command(opts.FrpcExe, "-c", opts.ConfigPath)
	cmd.Dir = filepath.Dir(opts.FrpcExe)
	hideWindow(cmd) // 不弹黑窗口（CREATE_NO_WINDOW）
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		in.transition(StateFailed, "打开进程输出管道失败: "+err.Error())
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		in.transition(StateFailed, "打开进程错误管道失败: "+err.Error())
		return err
	}
	if err := cmd.Start(); err != nil {
		in.transition(StateFailed, "进程启动失败: "+err.Error())
		return err
	}

	in.mu.Lock()
	in.cmd = cmd          // 立即登记，保证 Starting 窗口内 Stop 可触达进程
	in.pid = uint32(cmd.Process.Pid)
	in.exitCode = 0
	in.errMsg = ""
	in.startedAt = time.Now()
	in.stoppedAt = time.Time{}
	in.mu.Unlock()

	// JobObject 绑定：句柄随 HubKit 退出关闭 → 内核强杀整个进程树（防孤儿兜底）。
	// 创建或绑定失败视为致命：拒绝裸跑 frpc（宁可启动失败也不留失控进程）。
	job, jerr := in.jobAPI.Create()
	if jerr != nil {
		_ = cmd.Process.Kill()
		go in.wait()
		in.transition(StateFailed, "创建 Job Object 失败: "+jerr.Error())
		return fmt.Errorf("创建 Job Object 失败: %w", jerr)
	}
	if aerr := job.Assign(in.pid); aerr != nil {
		job.Close()
		_ = cmd.Process.Kill()
		go in.wait()
		in.transition(StateFailed, "JobObject 绑定失败: "+aerr.Error())
		return fmt.Errorf("JobObject 绑定失败: %w", aerr)
	}

	in.mu.Lock()
	in.job = job
	in.mu.Unlock()

	go in.pump(stdout)
	go in.pump(stderr)
	go in.wait()
	in.transition(StateRunning, "")
	return nil
}

// Stop 停止实例：优先 TerminateJobObject 连子进程一起强杀，回退 TerminateProcess。
// 幂等：非运行中的实例直接返回。
func (in *Instance) Stop() error {
	in.startMu.Lock()
	defer in.startMu.Unlock()

	in.mu.Lock()
	if in.state != StateRunning && in.state != StateStarting {
		in.mu.Unlock()
		return nil
	}
	in.stopping = true
	cmd, job := in.cmd, in.job
	in.mu.Unlock()

	if job != nil {
		if err := job.Terminate(1); err == nil {
			return nil // wait goroutine 将收尾状态
		}
	}
	if cmd != nil && cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return fmt.Errorf("实例没有可终止的进程（可能仍在启动）")
}

// Snapshot 返回当前状态快照。
func (in *Instance) Snapshot() Snapshot {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.snapshotLocked()
}

// Logs 返回最近 n 行日志（n <= 0 时全部）。
func (in *Instance) Logs(n int) []string {
	return in.logs.Last(n)
}

// RunningFrom 已运行时长；未运行时返回 0。
func (in *Instance) RunningDuration() time.Duration {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.state != StateRunning || in.startedAt.IsZero() {
		return 0
	}
	return time.Since(in.startedAt)
}

// ---------- 内部实现 ----------

// snapshotLocked 前置条件：已持 in.mu。
func (in *Instance) snapshotLocked() Snapshot {
	return Snapshot{
		ProjectID:   in.projectID,
		ProjectName: in.projectName,
		Version:     in.version,
		State:       in.state,
		PID:         in.pid,
		ExitCode:    in.exitCode,
		Error:       in.errMsg,
		StartedAt:   in.startedAt,
		StoppedAt:   in.stoppedAt,
	}
}

// transition 切换状态并通知回调（锁外执行回调，避免阻塞状态访问方）。
func (in *Instance) transition(s State, errMsg string) {
	var snap Snapshot
	in.mu.Lock()
	in.state = s
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

// wait 阻塞等待进程退出，回收资源并结算最终状态。
func (in *Instance) wait() {
	err := in.cmd.Wait()
	code := 0
	if err != nil && in.cmd.ProcessState != nil {
		code = in.cmd.ProcessState.ExitCode()
	}

	in.mu.Lock()
	in.exitCode = code
	in.stoppedAt = time.Now()
	stopped := in.stopping
	prev := in.state
	if in.job != nil {
		_ = in.job.Close()
		in.job = nil
	}
	in.cmd = nil
	in.mu.Unlock()

	switch {
	case stopped:
		// 手动停止（含 JobObject 绑定失败后 Kill 的场景）：消息由发起方决定
		in.transition(StateStopped, "已手动停止")
	case code == 0 && prev == StateRunning:
		in.transition(StateStopped, "")
	case prev != StateFailed:
		// 自然异常退出，或启动瞬间崩溃
		in.transition(StateFailed, fmt.Sprintf("frpc 进程异常退出（退出码 %d）", code))
	default:
		// 启动失败路径（JobObject 绑定失败等）：保留更精确的错误信息，只刷新快照
		in.mu.Lock()
		snap := in.snapshotLocked()
		in.mu.Unlock()
		if in.cb.OnState != nil {
			in.cb.OnState(snap)
		}
	}
}

// pump 逐行搬运子进程输出到环形日志。
func (in *Instance) pump(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			in.writeLog(line)
		}
		if err != nil { // io.EOF 或管道损坏
			return
		}
	}
}

// writeLog 统一入口：脱敏 → 环形缓冲 → 回调。
func (in *Instance) writeLog(line string) {
	in.mu.Lock()
	redact := append([]string(nil), in.redact...)
	in.mu.Unlock()

	line = redactText(line, redact)
	in.logs.Write(line)
	if in.cb.OnLog != nil {
		in.cb.OnLog(in.projectID, line)
	}
}

// redactText 将秘密串替换为 ***，防止 token 泄漏进日志与日志回放。
func redactText(line string, secrets []string) string {
	for _, s := range secrets {
		if s != "" && strings.Contains(line, s) {
			line = strings.ReplaceAll(line, s, "***")
		}
	}
	return line
}