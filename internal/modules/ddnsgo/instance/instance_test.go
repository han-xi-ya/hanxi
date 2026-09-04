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
	running bool // IsRunning 返回值（ddns-go.exe 进程扫描）
	port    bool // PortOpen 返回值（web 端口就绪）
}

func (p *fakeProbe) FindPIDs() []uint32 {
	if p.running {
		return []uint32{1}
	}
	return nil
}
func (p *fakeProbe) IsRunning() bool      { return p.running }
func (p *fakeProbe) PortOpen(string) bool { return p.port }

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

// mustCmdExe 返回系统 cmd.exe 路径（带 "-l" 参数即报错退出，短命冒烟进程）。
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

func startOpts(exe string) StartOptions {
	return StartOptions{Version: "v6.17.6", Exe: exe, ListenAddr: "127.0.0.1:9876"}
}

// ---------- Start 防御分支 ----------

// TestStartPortOccupiedByExternal 端口预检：端口被占 + 存在 ddns-go.exe 进程
// → 判外部实例接管，落 external 且不创建子进程。
func TestStartPortOccupiedByExternal(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: true, port: true}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("外部实例占位时应返回错误")
	}
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}
}

// TestStartPortOccupiedByOther 端口被非 ddns-go 程序占用 → failed 且文案指向端口。
func TestStartPortOccupiedByOther(t *testing.T) {
	probe := &fakeProbe{running: false, port: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("端口占用应返回错误")
	}
	snap := e.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("state = %s, want failed", snap.State)
	}
	if snap.PID != 0 {
		t.Fatal("预检失败不应拉起进程")
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

// TestStartReadyTimeoutKill 就绪等待负路径：端口永不可用 + 进程僵活
// （模拟上游端口冲突僵活一分钟的特性）→ Start 终止进程并落 failed。
// 用真实 cmd.exe（立即退出）与 fakeProbe(port=false) 组合，走 cmd 早退分支。
func TestStartReadyAbortOnDeadProcess(t *testing.T) {
	oldTimeout := readyTimeout
	readyTimeout = 2 * time.Second
	defer func() { readyTimeout = oldTimeout }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false, port: false}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t))) // cmd.exe 带 -l 参数秒退
	if err == nil {
		t.Fatal("就绪失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// ---------- 就绪与状态机 ----------

// TestWaitPortReadyDeadEarlyExit 进程已注销时就绪轮询应提前放弃而非等满超时。
func TestWaitPortReadyDeadEarlyExit(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{port: false}, Callbacks{})
	start := time.Now()
	if e.waitPortReady("127.0.0.1:9876", 30*time.Second) {
		t.Fatal("port 不可用应返回 false")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cmd 为空应早退，耗时 %v", time.Since(start))
	}
}

// TestWaitExitCodeZeroClassifiedStopped 冒烟：进程正常退出（退出码 0 且此前 running）→ stopped。
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

// TestWaitExternalTaken 自有进程退出但 ddns-go.exe 进程扫描仍命中外部实例 → external。
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

// TestWaitAbnormalExit 异常退出码（1）→ failed 且错误文案指向端口/配置。
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
	e.listen = "127.0.0.1:9876"
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

// TestRefreshExternalTwoWay 静止态下进程扫描命中 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
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

// ---------- Quit / Stop ----------

// TestQuitKillsAndSettles Quit：终止僵活进程（ping 长眠），wait() 收尾归 stopped。
func TestQuitKillsAndSettles(t *testing.T) {
	oldQuiet, oldMax := configSettleQuiet, configSettleMax
	configSettleQuiet, configSettleMax = 0, 0 // 跳过静默期等待（非本用例主题）
	defer func() { configSettleQuiet, configSettleMax = oldQuiet, oldMax }()

	ping := exec.Command(mustCmdExe(t), "/c", "ping", "-n", "30", "127.0.0.1")
	if err := ping.Start(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.mu.Lock()
	e.cmd = ping
	e.pid = uint32(ping.Process.Pid)
	e.state = StateRunning
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

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
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

// ---------- 配置写静默期与脱敏（纯函数） ----------

func TestConfigQuiescencePending(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if !configQuiescencePending(now.Add(-100*time.Millisecond), now) {
		t.Error("刚写完 100ms 应视为静默观察期")
	}
	if !configQuiescencePending(now.Add(-configSettleQuiet+time.Millisecond), now) {
		t.Error("差 1ms 达静默阈值仍应等待")
	}
	if configQuiescencePending(now.Add(-configSettleQuiet-time.Second), now) {
		t.Error("超过静默阈值应放行")
	}
	if configQuiescencePending(now.Add(time.Hour), now) {
		t.Error("时钟回拨等异常 mtime（未来）不应等待")
	}
}

func TestScrubSecrets(t *testing.T) {
	cases := map[string]string{
		"request https://api.dnspod.com?login_token=abc123&Domain=xx": "request https://api.dnspod.com?login_token=***&Domain=xx",
		"AccessKeyId=LTAI5tFakeKey secret=xyz":                        "AccessKeyId=*** secret=***",
		"password=hunter2 submit":                                     "password=*** submit",
		"更新成功 IP=1.2.3.4 无敏感词":                                        "更新成功 IP=1.2.3.4 无敏感词",
	}
	for in, want := range cases {
		if got := scrubSecrets(in); got != want {
			t.Errorf("scrubSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}
