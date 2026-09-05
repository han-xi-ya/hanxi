//go:build windows

package instance

import (
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running          bool   // IsRunning 返回值
	ready            bool   // WaitForReady 立即返回值
	windowVisible    bool   // HasVisibleWindow 返回值（Quit 三态分叉信号）
	calledVisibleFor uint32 // 记录最近一次查询的 pid
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) HasVisibleWindow(pid uint32) bool {
	p.calledVisibleFor = pid
	return p.windowVisible
}

type fakeJob struct{ assignErr error }

func (f *fakeJob) Assign(pid uint32) error        { return f.assignErr }
func (f *fakeJob) Close() error                   { return nil }
func (f *fakeJob) Terminate(code uint32) error    { return nil }
func (f *fakeJob) SetAllowKillOnClose(bool) error { return nil }

// fakeJobAPI 通过 createErr 控制 Create 失败，job.assignErr 控制 Assign 失败。
type fakeJobAPI struct {
	createErr error
	job       *fakeJob
}

func (f *fakeJobAPI) Create() (platform.Job, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.job == nil {
		f.job = &fakeJob{}
	}
	return f.job, nil
}

// eventRecorder 记录状态广播，便于断言事件序列。
type eventRecorder struct {
	mu     sync.Mutex
	states []Snapshot
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{OnState: func(s Snapshot) {
		r.mu.Lock()
		r.states = append(r.states, s)
		r.mu.Unlock()
	}}
}

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// mustCmdExe 返回系统 cmd.exe 路径（短命冒烟进程）。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal("cmd.exe 不可用")
	}
	return path
}

// waitState 轮询直至引擎达到目标状态（或超时）。
func waitState(t *testing.T, e *Engine, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := e.Snapshot().State; got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %s 超时，当前 %s", want, e.Snapshot().State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// compressGrace 单测压缩两级宽限时长，测试后恢复生产值。
func compressGrace(t *testing.T) {
	t.Helper()
	oldClose, oldCleanup := closeGracePeriod, cleanupGracePeriod
	closeGracePeriod, cleanupGracePeriod = 150*time.Millisecond, 250*time.Millisecond
	t.Cleanup(func() { closeGracePeriod, cleanupGracePeriod = oldClose, oldCleanup })
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v2.15.0", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	rec := &eventRecorder{}
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v2.15.0", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestWaitExitCodeZeroClassifiedStopped 冒烟：进程正常退出（退出码 0 且此前 running）→ stopped。
func TestWaitExitCodeZeroClassifiedStopped(t *testing.T) {
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()

	go e.wait()
	waitState(t, e, StateStopped)
}

// TestWaitExternalTaken 冷启动竞速：我们的进程信使化自退（exit 0）后互斥体仍被
// 外部主实例持有 → external。
func TestWaitExternalTaken(t *testing.T) {
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()

	go e.wait()
	waitState(t, e, StateExternal)
	if snap := e.Snapshot(); !snap.External {
		t.Fatalf("快照异常: %+v", snap)
	}
}

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误提示指向 Win10 版本门槛。
func TestWaitAbnormalExit(t *testing.T) {
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()

	go e.wait()
	waitState(t, e, StateFailed)
	snap := e.Snapshot()
	if snap.ExitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", snap.ExitCode)
	}
	if snap.Error == "" {
		t.Error("failed 应携带错误说明")
	}
}

// TestRefreshExternalTwoWay 静止态下互斥体出现 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	// stopped 且互斥体不在 → 无变化不广播
	e.RefreshExternal()
	if got := rec.count(); got != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", got)
	}

	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}

	probe.running = false
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的正是自己）
	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestQuitExitedGraceful 短命进程：WM_CLOSE 后（找不到窗口，静默）宽限内自然退出 → exited。
func TestQuitExitedGraceful(t *testing.T) {
	compressGrace(t)
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	res, err := e.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res != QuitExited {
		t.Fatalf("Quit 结果 = %s, want exited", res)
	}
	waitState(t, e, StateStopped)
}

// TestQuitWindowUp 进程存活且有可见窗口（询问对话框/用户取消）→ windowUp，
// 且托管关系保留（不强杀），显式 Stop 才终结。
func TestQuitWindowUp(t *testing.T) {
	compressGrace(t)
	cmd := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{windowVisible: true}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	res, err := e.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res != QuitWindowUp {
		t.Fatalf("Quit 结果 = %s, want windowUp", res)
	}
	if snap := e.Snapshot(); snap.State != StateRunning {
		t.Fatalf("windowUp 不得终结进程，state = %s", snap.State)
	}
	if probeVisible := e.probe.(*fakeProbe).calledVisibleFor; probeVisible != e.Snapshot().PID {
		t.Errorf("窗口探测应携带自有 pid: %d vs %d", probeVisible, e.Snapshot().PID)
	}

	// 强杀唯一入口：Stop
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
}

// TestQuitHiddenTray 进程存活且无可见窗口（WhenClose=MINIMIZE 收入托盘，或收尾后
// 仍不退出）→ 两级宽限耗尽后返回 hidden，进程保留托管。
func TestQuitHiddenTray(t *testing.T) {
	compressGrace(t)
	cmd := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{windowVisible: false}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	res, err := e.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res != QuitHidden {
		t.Fatalf("Quit 结果 = %s, want hidden", res)
	}
	if snap := e.Snapshot(); snap.State != StateRunning {
		t.Fatalf("hidden 不得强杀进程，state = %s", snap.State)
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
}

// TestQuitStopsPollingOnNaturalExit 收尾观察期内进程自然退出 → exited（不误报 hidden）。
func TestQuitStopsPollingOnNaturalExit(t *testing.T) {
	oldClose, oldCleanup := closeGracePeriod, cleanupGracePeriod
	closeGracePeriod, cleanupGracePeriod = 100*time.Millisecond, 5*time.Second
	defer func() { closeGracePeriod, cleanupGracePeriod = oldClose, oldCleanup }()

	// 300ms 后自行退出的进程：第一阶段错过，第二阶段观察命中
	cmd := exec.Command(mustCmdExe(t), "/c", "ping -n 2 127.0.0.1 >nul && exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{windowVisible: false}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	start := time.Now()
	res, err := e.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res != QuitExited {
		t.Fatalf("Quit 结果 = %s, want exited", res)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("观察期应进程退出即返回，耗时 %v", elapsed)
	}
}

// TestStopQuitIdempotentWhenStopped 静止态 Quit/Stop 均无副作用（external 由 service 层分流，
// 引擎侧非运行态统一 QuitExited）。
func TestStopQuitIdempotentWhenStopped(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if res, err := e.Quit(); err != nil || res != QuitExited {
		t.Fatalf("Quit(stopped) = %v, %v; want exited, nil", res, err)
	}
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
}

// TestOpenWindowMessenger 信使拉起冒烟：真实 cmd.exe 短命进程，仅验证不阻塞不报错。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t)); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}
