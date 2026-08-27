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
	running    bool // IsEverythingRunning 返回值
	ready      bool // WaitForEverythingReady 立即返回值
	windowOpen bool // IsSearchWindowOpen 返回值
}

func (p *fakeProbe) IsEverythingRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForEverythingReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsSearchWindowOpen() bool                  { return p.windowOpen }

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

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// mustCmdExe 返回系统 cmd.exe 路径（无参数启动即遇 EOF 立即退出，可用作短命冒烟进程）。
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
	err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: ModeBackground})
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
	err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: ModeWindow})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartInvalidMode(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: "illegal"}); err == nil {
		t.Fatal("非法运行模式应被拒绝")
	}
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: "", Mode: ModeWindow}); err == nil {
		t.Fatal("空 exe 应被拒绝")
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

// TestWaitExternalTaken 冷启动竞速：我们的进程自退后探测仍命中 → external。
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
	if !snap.External {
		t.Fatalf("快照异常: %+v", snap)
	}
}

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误提示指向索引库。
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

// TestOpenWindowMessenger 自有运行态：信使唤窗成功并翻新运行模式为 window；stopped 态信使照发（唤外部）。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	exe := mustCmdExe(t)

	e.mu.Lock()
	e.state = StateRunning
	e.mode = ModeBackground
	e.mu.Unlock()
	opened, err := e.OpenWindow(exe)
	if err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v", opened, err)
	}
	if snap := e.Snapshot(); snap.Mode != ModeWindow {
		t.Fatalf("自有实例唤窗后 mode 应更新为 window, 实际 %s", snap.Mode)
	}
}

// TestQuitGracefulAndForceKillFallback Quit 两条路径：
//  1. -quit 信使后进程自然退出 → stopped（优雅路径）；
//  2. 进程赖着不走 → 超时后强杀兜底 → stopped。
//
// 测试压缩 quitGracePeriod 避免 5s 真等待。
func TestQuitGracefulAndForceKillFallback(t *testing.T) {
	oldGrace := quitGracePeriod
	defer func() { quitGracePeriod = oldGrace }()

	newHeldCmd := func(t *testing.T, e *Engine) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(mustCmdExe(t), "/c", "ping 127.0.0.1 -n 30 >nul") // 长命进程
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		e.mu.Lock()
		e.cmd = cmd
		e.pid = uint32(cmd.Process.Pid)
		e.state = StateRunning
		e.startedAt = time.Now()
		e.mu.Unlock()
		go e.wait() // 状态迁移的唯一来源
		return cmd
	}

	// 路径 1：进程提前退出 → 优雅路径（quit 信使照发，poll 提前返回）
	{
		quitGracePeriod = 5 * time.Second
		e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
		cmd := exec.Command(mustCmdExe(t), "/c", "exit 0")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		e.mu.Lock()
		e.cmd = cmd
		e.pid = uint32(cmd.Process.Pid)
		e.state = StateRunning
		e.mu.Unlock()
		go e.wait()
		if err := e.Quit(); err != nil {
			t.Fatalf("Quit 优雅路径: %v", err)
		}
		waitState(t, e, StateStopped)
	}

	// 路径 2：进程赖着不走 → 强杀兜底
	{
		quitGracePeriod = 300 * time.Millisecond
		e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
		newHeldCmd(t, e)
		if err := e.Quit(); err != nil {
			t.Fatalf("Quit 兜底路径: %v", err)
		}
		waitState(t, e, StateStopped)
		if snap := e.Snapshot(); snap.Error != "" {
			t.Fatalf("手动退出不应残留错误信息, 实际 %q", snap.Error)
		}
	}
}

func TestQuitIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
}

// TestRefreshExternalTwoWay 静止态下实例出现 → external，消失 → stopped；running 态不受影响。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	e.RefreshExternal()
	if got := rec.count(); got != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", got)
	}

	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}
	if got := e.Snapshot().Mode; got != "" {
		t.Fatalf("外部实例不应揣测运行模式, mode = %q", got)
	}

	probe.running = false
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestWaitReady 委托探针。
func TestWaitReady(t *testing.T) {
	probe := &fakeProbe{ready: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("探针就绪时应返回 true")
	}
	probe.ready = false
	if e.WaitReady(time.Second) {
		t.Fatal("探针未就绪时应返回 false")
	}
}
