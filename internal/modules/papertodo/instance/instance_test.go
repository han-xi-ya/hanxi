//go:build windows

package instance

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running bool // IsRunning 返回值
	ready   bool // WaitForReady 立即返回值
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }

type fakeJob struct {
	assignErr    error
	terminateErr error
	terminated   int    // forceKill 计数（断言兜底是否触发）
	onTerminate  func() // Terminate 的真实副作用挂钩（杀进程用）
}

func (f *fakeJob) Assign(pid uint32) error { return f.assignErr }
func (f *fakeJob) Close() error            { return nil }
func (f *fakeJob) Terminate(code uint32) error {
	f.terminated++
	if f.onTerminate != nil {
		f.onTerminate() // 模拟 TerminateProcess：让自有进程真正消亡
	}
	return f.terminateErr
}
func (f *fakeJob) SetAllowKillOnClose(bool) error { return nil }

// fakeJobAPI 通过 createErr 控制 Create 失败，job.assignErr 控制 Assign 失败。
type fakeJobAPI struct {
	mu        sync.Mutex
	createErr error
	job       *fakeJob
}

func (f *fakeJobAPI) Create() (platform.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// captureMessenger 替换 launchMessenger 记录调用参数，避免测试真实拉起信使进程。
func captureMessenger(t *testing.T) *[]string {
	t.Helper()
	old := launchMessenger
	var calls []string
	launchMessenger = func(exe string, args ...string) error {
		calls = append(calls, exe+" "+joinArgs(args))
		return nil
	}
	t.Cleanup(func() { launchMessenger = old })
	return &calls
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
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

func TestStartValidate(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Exe: ""}); err == nil {
		t.Fatal("空 Exe 应报错")
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v3.31", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "v3.31", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestOpenWindowSendsShow 唤窗必须经 show 命令信使，且不真实拉起进程。
func TestOpenWindowSendsShow(t *testing.T) {
	calls := captureMessenger(t)
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.OpenWindow(`C:\x\PaperTodo.exe`); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if len(*calls) != 1 || !endsWith(*calls, "show") {
		t.Fatalf("应发出一条 show 信使，实际 %v", *calls)
	}
}

// TestHidePapersSendsHide 收拢走 hide 命令。
func TestHidePapersSendsHide(t *testing.T) {
	calls := captureMessenger(t)
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.HidePapers(`C:\x\PaperTodo.exe`); err != nil {
		t.Fatalf("HidePapers: %v", err)
	}
	if len(*calls) != 1 || !endsWith(*calls, "hide") {
		t.Fatalf("应发出一条 hide 信使，实际 %v", *calls)
	}
}

func endsWith(calls []string, suffix string) bool {
	return len(calls) == 1 && strings.HasSuffix(calls[0], " "+suffix)
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

// TestWaitAbnormalExit 异常退出码（1）→ failed，错误提示指向 no-runtime 变体的 .NET 10 桌面运行时。
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
	if !strings.HasSuffix(snap.Error, "（环境检测页可查看）") {
		t.Errorf("失败文案应提示 .NET 运行库，实际: %q", snap.Error)
	}
}

// TestQuitSendsExitAndGraceful 自有实例运行中 Quit：恰派一条 exit 信使，
// 主实例（模拟）自退后宽限内收敛 stopped，不触发强杀兜底。
func TestQuitSendsExitAndGraceful(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 2 * time.Second
	defer func() { closeGracePeriod = old }()

	var mu sync.Mutex
	var calls []string
	oldLM := launchMessenger
	defer func() { launchMessenger = oldLM }()

	cmd := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "10", "127.0.0.1")
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

	// 注入信使：记录命令，并模拟"主实例收到 exit → 保存后自退"——
	// 必须等 Quit 置位 stopping 之后再杀进程，否则 wait() 会误分类为异常退出。
	launchMessenger = func(exe string, args ...string) error {
		mu.Lock()
		calls = append(calls, joinArgs(args))
		mu.Unlock()
		go func() {
			for range 100 {
				e.mu.Lock()
				stopping := e.stopping
				e.mu.Unlock()
				if stopping {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			_ = cmd.Process.Kill()
		}()
		return nil
	}

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "exit" {
		t.Errorf("Quit 应恰发一条 exit 信使，实际 %v", calls)
	}
}

// TestQuitForceKillFallback 主实例对 exit 无响应（进程不退出）→ 宽限超时强杀兜底。
func TestQuitForceKillFallback(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	oldLM := launchMessenger
	launchMessenger = func(exe string, args ...string) error { return nil } // 吞掉，进程不退
	defer func() { launchMessenger = oldLM }()

	cmd := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	fj := &fakeJob{}
	fj.onTerminate = func() { _ = cmd.Process.Kill() } // 模拟 TerminateProcess 的真实效果
	e := NewEngine(&fakeJobAPI{job: fj}, &fakeProbe{running: false}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.job = fj // 手动装配场无 Start：直接注入 Job，forceKill 走 Terminate 分支
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.mu.Unlock()
	go e.wait()

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	if fj.terminated == 0 {
		t.Error("exit 无响应时应触发 JobObject 强杀兜底")
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
