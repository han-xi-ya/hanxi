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
	running    bool // IsRunning 返回值
	ready      bool // WaitForReady 立即返回值
	windowOpen bool // IsMainWindowOpen 返回值（空闲退出豁免信号）
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsMainWindowOpen(uint32) bool    { return p.windowOpen }

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

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// mustCmdExe 返回系统 cmd.exe 路径（无参数启动即遇 GPIO 立即退出，可用作短命冒烟进程）。
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

func TestStartCreateJobFail(t *testing.T) {
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustCmdExe(t)}); err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustCmdExe(t)}); err == nil {
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

// TestWaitExternalTaken 冷启动竞速：我们的进程自退后互斥体仍被外部主实例持有 → external。
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

// TestWaitAbnormalExit 异常退出码（1）→ failed。
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
	if snap := e.Snapshot(); snap.ExitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", snap.ExitCode)
	}
}

// TestRefreshExternalTwoWay 静止态下互斥体出现 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{OnState: func(Snapshot) { rec.mu.Lock(); rec.states = append(rec.states, Snapshot{}); rec.mu.Unlock() }})

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

	// running 时不探测（探测到的是自己）
	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestQuitGraceFallback 冒烟：Quit 对长命进程走强杀兜底，短命进程宽限内自然收尾。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	// ping -t 不退出，验证强杀兜底路径
	ping := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := ping.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.mu.Lock()
	e.cmd = ping
	e.pid = uint32(ping.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)

	// 短命进程：Quit 后宽限内自然退走优雅路径
	quick := exec.Command(mustCmdExe(t), "/c", "exit", "0")
	if err := quick.Start(); err != nil {
		t.Fatal(err)
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	e2.mu.Lock()
	e2.cmd = quick
	e2.pid = uint32(quick.Process.Pid)
	e2.state = StateRunning
	e2.mu.Unlock()
	go e2.wait()

	if err := e2.Quit(); err != nil {
		t.Fatalf("Quit(短命): %v", err)
	}
	waitState(t, e2, StateStopped)
}

// TestStopIdempotent 停止幂等且覆盖 Quit：静止态调用无副作用。
func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestOpenWindowMessenger 信使拉起冒烟：真实 cmd.exe 短命进程，仅验证不阻塞不报错。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t)); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}

// TestIsMainWindowOpen 窗口豁免信号：仅 running 态向 probe 传递 pid。
func TestIsMainWindowOpen(t *testing.T) {
	probe := &fakeProbe{windowOpen: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	e.mu.Lock()
	e.state = StateRunning
	e.pid = 1234
	e.mu.Unlock()
	if !e.IsMainWindowOpen() {
		t.Fatal("running + probe 可见时应为 true")
	}
	probe.windowOpen = false
	if e.IsMainWindowOpen() {
		t.Fatal("probe 不可见时应为 false")
	}
	// 静止态不计窗口
	e.mu.Lock()
	e.state = StateStopped
	e.mu.Unlock()
	probe.windowOpen = true
	if e.IsMainWindowOpen() {
		t.Fatal("stopped 状态应恒为 false")
	}
}
