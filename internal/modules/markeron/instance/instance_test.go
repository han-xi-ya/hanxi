//go:build windows

package instance

import (
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"hubkit/internal/platform"
	"hubkit/internal/platform/windows"
)

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running bool // IsMarkerOnRunning 返回值
	ready   bool // WaitForMarkerOnReady 立即返回值
}

func (p *fakeProbe) IsMarkerOnRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForMarkerOnReady(time.Duration) bool { return p.ready }

type fakeJob struct{ assignErr error }

func (f *fakeJob) Assign(pid uint32) error     { return f.assignErr }
func (f *fakeJob) Close() error                { return nil }
func (f *fakeJob) Terminate(code uint32) error { return nil }

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

func (r *eventRecorder) last() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return Snapshot{}
	}
	return r.states[len(r.states)-1]
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
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v2.9.4", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "v2.9.4", Exe: mustCmdExe(t)})
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
	snap := e.Snapshot()
	if !snap.External || snap.Drawing {
		t.Fatalf("快照异常: %+v", snap)
	}
}

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误提示指向 WebView2。
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

// TestToggleDrawingParity 自有运行实例的信使翻转奇偶推算；非运行态不翻转。
func TestToggleDrawingParity(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	exe := mustCmdExe(t)

	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	if err := e.Toggle(exe); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if !e.Snapshot().Drawing {
		t.Fatal("第一次切换后 drawing 应为 true（Hidden→Drawing）")
	}
	if err := e.Toggle(exe); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if e.Snapshot().Drawing {
		t.Fatal("第二次切换后 drawing 应为 false")
	}

	// stopped 状态下翻转无效
	e.mu.Lock()
	e.state = StateStopped
	e.drawing = false
	e.mu.Unlock()
	if err := e.Toggle(exe); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if e.Snapshot().Drawing {
		t.Fatal("stopped 状态不应翻转 drawing")
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

// TestStopIdempotent 停止幂等：stopped 状态下调用无副作用。
func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
}
