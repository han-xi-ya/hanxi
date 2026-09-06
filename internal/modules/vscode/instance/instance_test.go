//go:build windows

package instance

import (
	"errors"
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
	running bool // IsRunning 返回值
	ready   bool // WaitForReady 立即返回值
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }

type fakeJob struct{ assignErr error }

func (f *fakeJob) Assign(pid uint32) error        { return f.assignErr }
func (f *fakeJob) Close() error                   { return nil }
func (f *fakeJob) Terminate(code uint32) error    { return nil }
func (f *fakeJob) SetAllowKillOnClose(bool) error { return nil }

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

func newWindowsJobAPI() platform.JobAPI { return windows.NewJobAPI() }

// mustCmdExe 系统 cmd.exe（无参启动即退，短命冒烟进程）。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal("cmd.exe 不可用")
	}
	return path
}

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
	err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "1.136.1", Form: "installer", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartEmptyExe(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Form: "portable"}); err == nil {
		t.Fatal("空 exe 应被 validate 拒绝")
	}
}

// TestWaitExitCodeZeroClassifiedStopped 进程正常退出（关窗即退）→ stopped。
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

// TestWaitExternalTaken 冷启动竞速：我们的进程信使化自退后探测信号仍在 → external。
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

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误文案给出排查指引。
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

// TestRefreshExternalTwoWay 静止态下探测信号出现 → external，消失 → stopped。
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

// TestQuitGraceFallback WM_CLOSE 找不到窗口 → 宽限后强杀兜底；短命进程走优雅路径。
func TestQuitGraceFallback(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	// ping 不自然退出，验证强杀兜底路径
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

// ---------- 便携版路径前缀匹配边界 ----------

func TestIsUnderDir(t *testing.T) {
	root := filepath.Join("D:", "hanxi", "versions")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "vscode_1.136.1", "Code.exe"), true},
		{filepath.Join(root, "Code.exe"), true},
		// 前缀相似但非目录边界：D:\hanxi\versions-evil 不得命中 D:\hanxi\versions
		{"D:\\hanxi\\versions-evil\\Code.exe", false},
		{root, false},                                   // 目录自身不算其内部
		{"D:\\vscode\\Code.exe", false},                 // 安装版目录不误入便携前缀
		{strings.ToUpper(root) + "\\X\\Code.exe", true}, // Windows 路径大小写不敏感
	}
	for _, c := range cases {
		if got := isUnderDir(c.path, root); got != c.want {
			t.Errorf("isUnderDir(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
