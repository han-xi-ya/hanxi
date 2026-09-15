//go:build windows

package instance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	focus   bool // FocusWindow 返回值（是否找到可见窗口）
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsWindowOpen() bool              { return p.focus }
func (p *fakeProbe) FocusWindow() bool               { return p.focus }

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

// mustCmdExe 返回系统 cmd.exe 路径（用作短命/常驻冒烟进程）。
// LookPath 失败时经 %SystemRoot%\System32 兜底——git-bash 等非系统 PATH
// 环境下子进程 PATH 不含 System32，不能让环境差异掩盖引擎行为断言。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		if root := os.Getenv("SystemRoot"); root != "" {
			cand := filepath.Join(root, "System32", "cmd.exe")
			if fi, serr := os.Stat(cand); serr == nil && !fi.IsDir() {
				return cand
			}
		}
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

// compressSettle 单测压缩外部接管静默期（生产 500ms 会拖慢每个分类用例）。
func compressSettle(t *testing.T) {
	t.Helper()
	old := externalSettle
	externalSettle = 10 * time.Millisecond
	t.Cleanup(func() { externalSettle = old })
}

func TestStartCreateJobFail(t *testing.T) {
	compressSettle(t)
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	compressSettle(t)
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	rec := &eventRecorder{}
	e := NewEngine(api, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartExeMustExist Start 对不存在路径直接失败（exec 报文件不存在，
// 不进入 Job 链路）。真实运行链路冒烟由 wait/Quit 系测试以注入 cmd 覆盖。
func TestStartExeMustExist(t *testing.T) {
	compressSettle(t)
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: `C:\不存在\paseo\Paseo.exe`, Detached: true}); err == nil {
		t.Fatal("不存在的 exe 应启动失败")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestWaitExitCodeZeroClassifiedStopped 冒烟：进程正常退出（退出码 0 且此前 running）→ stopped。
func TestWaitExitCodeZeroClassifiedStopped(t *testing.T) {
	compressSettle(t)
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

// TestWaitExternalTaken 冷启动竞速（共享数据同锁组核心场景）：我们的进程
// 拿不到锁自退（exit 0）后进程树仍存活 → external 接管。
func TestWaitExternalTaken(t *testing.T) {
	compressSettle(t)
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

// TestWaitAbnormalExit 异常退出码（1）→ failed 且携带错误说明。
func TestWaitAbnormalExit(t *testing.T) {
	compressSettle(t)
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
		t.Fatal("failed 状态应携带错误说明")
	}
}

// TestRefreshExternalTwoWay 静止态下进程出现 → external，消失 → stopped。
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

// TestFocusWindowPassthrough 唤窗优先直唤：fake 探针有窗则 FocusWindow 真、
// 无窗则假（service 据此才允许退到信使开新窗）。
func TestFocusWindowPassthrough(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{focus: true}, Callbacks{})
	if !e.FocusWindow() {
		t.Fatal("有窗时应唤起")
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{focus: false}, Callbacks{})
	if e2.FocusWindow() {
		t.Fatal("无窗时应返回 false")
	}
}

// TestQuitGraceFallback 冒烟：Quit 对短命进程宽限内收尾（WM_CLOSE 找不到
// 窗口 → 收紧的宽限内不能自然退则强杀兜底，两种路径最终都归 stopped）。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限与静默期：单测不能等生产值
	oldGrace, oldSettle := closeGracePeriod, externalSettle
	closeGracePeriod = 200 * time.Millisecond
	externalSettle = 10 * time.Millisecond
	defer func() { closeGracePeriod, externalSettle = oldGrace, oldSettle }()

	// cmd 内建死循环不退出（不依赖 ping.exe 等外部程序的 PATH 可用性），
	// 验证强杀兜底路径
	ping := exec.Command(mustCmdExe(t), "/c", "for /L %i in (1,1,2147483647) do @rem x")
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

// TestOpenMessenger 信使拉起冒烟：真实 cmd.exe 短命进程，仅验证不阻塞不报错。
func TestOpenMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.OpenMessenger(mustCmdExe(t)); err != nil {
		t.Fatalf("OpenMessenger: %v", err)
	}
}
