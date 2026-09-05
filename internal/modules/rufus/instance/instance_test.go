//go:build windows

package instance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	pids       []uint32
	windowOpen bool // IsMainWindowOpen 返回值
}

func (p *fakeProbe) FindPIDs() []uint32              { return p.pids }
func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsMainWindowOpen([]uint32) bool  { return p.windowOpen }

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

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartSeedsIni 冒烟：Start 先于进程创建在 exe 所在目录播种 rufus.ini。
// 用不存在的 exe 路径：Start 必然失败，但 seed 应已落盘（seed 在 cmd.Start 之前）。
func TestStartSeedsIni(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v4.15", Exe: filepath.Join(dir, exeName)}); err == nil {
		t.Fatal("不存在的 exe 应启动失败")
	}
	waitState(t, e, StateFailed)
	data, err := os.ReadFile(filepath.Join(dir, iniFileName))
	if err != nil {
		t.Fatalf("Start 应播种 %s: %v", iniFileName, err)
	}
	if !strings.Contains(string(data), "UpdateCheckInterval = -1") {
		t.Errorf("种子应包含禁用更新检查键: %q", data)
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

// TestWaitExternalTaken 冷启动竞速：我们的进程弹"已在运行"框退出后
// 互斥体/进程枚举仍命中外部实例 → external。
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

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误提示指向管理员权限。
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
	if !strings.Contains(snap.Error, "管理员") {
		t.Errorf("失败文案应指向管理员运行指引: %q", snap.Error)
	}
}

// TestRefreshExternalTwoWay 静止态下实例出现 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	// stopped 且进程不在 → 无变化不广播
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

// TestQuitGraceFallback 冒烟：Quit 宽限路径——长命进程走强杀兜底，
// 短命进程走宽限内自然退出的优雅路径。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	// ping -n 30 不退出，验证强杀兜底路径
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

	// 进程先自然退出，Quit 宽限轮询立刻观察到
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

// TestRestoreWindowNonRunning 非 running 态唤窗是安全 no-op（不 panic 不改状态）。
func TestRestoreWindowNonRunning(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.RestoreWindow()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}
	e.RestoreExternalWindow([]uint32{uint32(0)})
}

// TestIsMainWindowOpen running 且有可见窗口信号 → true；非 running → false。
func TestIsMainWindowOpen(t *testing.T) {
	probe := &fakeProbe{windowOpen: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	if e.IsMainWindowOpen() {
		t.Error("stopped 态不应报告窗口打开")
	}
	e.mu.Lock()
	e.state = StateRunning
	e.pid = uint32(os.Getpid())
	e.mu.Unlock()
	if !e.IsMainWindowOpen() {
		t.Error("running 态应透传探针结果")
	}
}

// TestIsRufusProcess 进程名形态判定：托管名、官方资产名命中；
// hogger（rufus.com）、无关进程、伪装前缀（rufuskiller.exe）绝不混入。
func TestIsRufusProcess(t *testing.T) {
	hits := []string{"rufus.exe", "RUFUS.EXE", "rufus-4.15p.exe", "rufus-4.9.exe", "rufus-4.15_x86.exe"}
	misses := []string{"rufus.com", "rufuskiller.exe", "rufus.txt", "explorer.exe", "rufus-", "rufu.exe"}
	for _, n := range hits {
		if !isRufusProcess(n) {
			t.Errorf("应命中: %s", n)
		}
	}
	for _, n := range misses {
		if isRufusProcess(n) {
			t.Errorf("不应命中: %s", n)
		}
	}
}
