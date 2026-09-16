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
	running bool // IsRunning 返回值（hanxi-ocr.exe 进程扫描）
	port    bool // PortOpen 返回值
	serving bool // IsOCRService 契约应答
}

func (p *fakeProbe) FindPIDs() []uint32 {
	if p.running {
		return []uint32{1}
	}
	return nil
}
func (p *fakeProbe) IsRunning() bool          { return p.running }
func (p *fakeProbe) PortOpen(string) bool     { return p.port }
func (p *fakeProbe) IsOCRService(string) bool { return p.serving }

type fakeJob struct {
	assignErr  error
	terminated int
	allowKill  *bool
}

func (f *fakeJob) Assign(pid uint32) error { return f.assignErr }
func (f *fakeJob) Close() error            { return nil }
func (f *fakeJob) Terminate(code uint32) error {
	f.terminated++
	return nil
}
func (f *fakeJob) SetAllowKillOnClose(enabled bool) error {
	f.allowKill = &enabled
	return nil
}

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

// mustCmdExe 返回系统 cmd.exe 路径（带 "serve -port" 参数即报错退出，短命冒烟进程）。
// LookPath 在类 Unix PATH（如 Git Bash 会话）下可能失效，回退 %SystemRoot%\System32。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		for _, root := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR"), `C:\Windows`} {
			if root == "" {
				continue
			}
			if cand := filepath.Join(root, "System32", "cmd.exe"); fileExists(cand) {
				return cand
			}
		}
		t.Fatal("cmd.exe 不可用")
	}
	return path
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
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

func startOpts(exe string) StartOptions {
	return StartOptions{Exe: exe, ListenAddr: "127.0.0.1:53120"}
}

// ---------- 纯函数 ----------

func TestBuildServeArgs(t *testing.T) {
	got := buildServeArgs("127.0.0.1:53121")
	want := []string{"serve", "-port", "53121"} // 上游 v0.2.0 启动参数协议
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

// ---------- Start 防御分支 ----------

// TestStartValidate 空路径/空地址直接拒绝，不动状态机。
func TestStartValidate(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{}); err == nil {
		t.Fatal("空参数应报错")
	}
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped（校验失败不动状态机）", got)
	}
}

// TestStartPortOccupiedByExternal 端口预检：端口开 + 契约应答
// → 判外部实例接管，落 external 且不创建子进程。
func TestStartPortOccupiedByExternal(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{port: true, serving: true}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("外部实例占位时应返回错误")
	}
	snap := e.Snapshot()
	if snap.State != StateExternal || !snap.External {
		t.Fatalf("state = %s external=%v, want external", snap.State, snap.External)
	}
	if snap.PID != 0 {
		t.Fatal("external 不应拉起自有进程")
	}
}

// TestStartPortOccupiedByOther 端口被非 hanxi-ocr 程序占用（契约不应答）
// → failed 且文案指向端口。
func TestStartPortOccupiedByOther(t *testing.T) {
	probe := &fakeProbe{port: true, serving: false}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("端口占用应返回错误")
	}
	snap := e.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("state = %s, want failed", snap.State)
	}
	if snap.Error == "" {
		t.Fatal("failed 应带中文说明")
	}
}

func TestStartCreateJobFail(t *testing.T) {
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartReadyAbortOnDeadProcess 就绪等待负路径：契约永不应答 +
// cmd.exe 秒退 → 引擎落 failed。
func TestStartReadyAbortOnDeadProcess(t *testing.T) {
	oldTimeout := readyTimeout
	readyTimeout = 2 * time.Second
	defer func() { readyTimeout = oldTimeout }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{port: false, serving: false}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t))) // cmd.exe 带 serve/-port 参数秒退
	if err == nil {
		t.Fatal("就绪失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// ---------- 就绪轮询与 wait 分类 ----------

// TestWaitReadyDeadEarlyExit 进程已注销（cmd==nil）时轮询应提前放弃而非等满超时。
func TestWaitReadyDeadEarlyExit(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	start := time.Now()
	if e.waitReady("127.0.0.1:53120", 30*time.Second) {
		t.Fatal("不可用应返回 false")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cmd 为空应早退，耗时 %v", time.Since(start))
	}
}

func TestWaitExitCodeZeroClassifiedStopped(t *testing.T) {
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
	waitState(t, e, StateStopped)
}

// TestWaitExternalTaken 自有进程退出但契约探测仍命中（别的 hanxi-ocr 活着）
// → external 而非误报失败。
func TestWaitExternalTaken(t *testing.T) {
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{serving: true}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.listen = "127.0.0.1:53120"
	e.mu.Unlock()

	go e.wait()
	waitState(t, e, StateExternal)
	if snap := e.Snapshot(); !snap.External {
		t.Fatalf("快照异常: %+v", snap)
	}
}

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误文案非空。
func TestWaitAbnormalExit(t *testing.T) {
	cmd := exec.Command(mustCmdExe(t), "/c", "exit", "1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.mu.Lock()
	e.cmd = cmd
	e.pid = uint32(cmd.Process.Pid)
	e.state = StateRunning
	e.listen = "127.0.0.1:53120"
	e.mu.Unlock()

	go e.wait()
	waitState(t, e, StateFailed)
	snap := e.Snapshot()
	if snap.ExitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", snap.ExitCode)
	}
	if snap.Error == "" {
		t.Fatal("failed 状态应带错误说明")
	}
}

// TestRefreshExternal 静止态契约命中 → external，消失 → stopped；
// 无变化不广播；running 不被改写；空地址保守跳过。
func TestRefreshExternal(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())
	const addr = "127.0.0.1:53120"

	// 空地址：任何静止态不动、不广播
	e.RefreshExternal("")
	if rec.count() != 0 || e.Snapshot().State != StateStopped {
		t.Fatal("空地址应整体跳过")
	}

	// stopped 且无服务 → 无变化不广播
	e.RefreshExternal(addr)
	if got := rec.count(); got != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", got)
	}

	probe.port, probe.serving = true, true
	e.RefreshExternal(addr)
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}

	probe.port, probe.serving = false, false
	e.RefreshExternal(addr)
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的正是自己）
	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	probe.port, probe.serving = true, true
	e.RefreshExternal(addr)
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// ---------- Stop ----------

// TestStopKillsAndSettles 真实 JobObject 终止僵活进程（ping 长眠），
// wait() 收尾归 stopped。
func TestStopKillsAndSettles(t *testing.T) {
	oldSettle := quitSettleWait
	quitSettleWait = 5 * time.Second
	defer func() { quitSettleWait = oldSettle }()

	ping := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := ping.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.mu.Lock()
	e.cmd = ping
	e.pid = uint32(ping.Process.Pid)
	e.state = StateRunning
	e.listen = "127.0.0.1:53120"
	job, err := e.jobAPI.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Assign(e.pid); err != nil {
		t.Fatal(err)
	}
	e.job = job
	e.mu.Unlock()
	go e.wait()

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
}

// TestStopIdempotent 静止态 Stop 无副作用无错误。
func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
}

// TestStartDetachedUnlinksJob Detached=true 走 SetAllowKillOnClose(false)。
func TestStartDetachedFlagPlumbing(t *testing.T) {
	fj := &fakeJob{}
	api := &fakeJobAPI{job: fj}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	// 让 Assign 成功、cmd 秒退；只验证 Detached 分支被走到（fj.allowKill 记录）
	err := e.Start(StartOptions{Exe: mustCmdExe(t), ListenAddr: "127.0.0.1:53120", Detached: true})
	if err == nil {
		t.Fatal("cmd 秒退本就该就绪失败")
	}
	if fj.allowKill == nil || *fj.allowKill != false {
		t.Fatalf("Detached 应调用 SetAllowKillOnClose(false)，allowKill = %v", fj.allowKill)
	}
}
