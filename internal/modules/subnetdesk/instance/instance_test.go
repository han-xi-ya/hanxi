//go:build windows

package instance

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// ---------- 测试用 fake（探针与 Job 注入） ----------

// fakeProbe 可编程的进程树探针：own/all 集合由测试随时改写，
// 模拟"内层出现→驻留→消失"的时间线。
type fakeProbe struct {
	mu      sync.Mutex
	own     []uint32
	all     []uint32
	visible bool
	focusN  int
}

func (p *fakeProbe) set(own, all []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.own = own
	p.all = all
}

func (p *fakeProbe) setOwn(own []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.own = own
}

func (p *fakeProbe) FindOwnPIDs([]uint32) []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.own...)
}

func (p *fakeProbe) FindInstancePIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.all...)
}

func (p *fakeProbe) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.all) > 0
}

func (p *fakeProbe) HasVisibleWindow([]uint32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.visible
}

func (p *fakeProbe) FocusWindows(pids []uint32) int {
	if len(pids) == 0 {
		return 0
	}
	return p.focusN
}

type fakeJob struct {
	assignErr  error
	onTerm     func() // Terminate 钩子：模拟进程树消失
	terminated bool
}

func (f *fakeJob) Assign(pid uint32) error { return f.assignErr }
func (f *fakeJob) Close() error            { return nil }
func (f *fakeJob) Terminate(code uint32) error {
	f.terminated = true
	if f.onTerm != nil {
		f.onTerm()
	}
	return nil
}
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

func (r *eventRecorder) has(state State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.states {
		if s.State == state {
			return true
		}
	}
	return false
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// mustCmdExe 返回系统 cmd.exe 路径（模拟短命外层 packer）。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal("cmd.exe 不可用")
	}
	return path
}

// withShortWaits 压缩引擎等待窗口，返回还原函数。
func withShortWaits(t *testing.T) func() {
	t.Helper()
	o1, o2, o3, o4, o5 := startGrace, earlyFailWait, quitGrace, phase1Poll, treePoll
	startGrace, earlyFailWait, quitGrace = time.Second, 100*time.Millisecond, time.Second
	phase1Poll, treePoll = 20*time.Millisecond, 20*time.Millisecond
	return func() {
		startGrace, earlyFailWait, quitGrace = o1, o2, o3
		phase1Poll, treePoll = o4, o5
	}
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
			t.Fatalf("等待状态 %s 超时，当前 %s（err=%q）", want, e.Snapshot().State, e.Snapshot().Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startEngine 以 cmd.exe 冒烟进程拉起引擎（模拟短命外层）。
func startEngine(t *testing.T, api platform.JobAPI, probe SubnetDeskProbe, rec Callbacks) *Engine {
	t.Helper()
	e := NewEngine(api, probe, rec)
	if err := e.Start(StartOptions{Version: "v1.3.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return e
}

func TestStartInnerNeverAppearsFails(t *testing.T) {
	defer withShortWaits(t)()
	rec := &eventRecorder{}
	api := &fakeJobAPI{job: &fakeJob{}}
	e := startEngine(t, api, &fakeProbe{}, rec.cb()) // own 恒空
	waitState(t, e, StateFailed)
	if snap := e.Snapshot(); !strings.Contains(snap.Error, "进程树") {
		t.Errorf("失败文案应指向解压/拦截排查: %q", snap.Error)
	}
}

func TestStartPromoteRunningThenTreeGoneStopped(t *testing.T) {
	defer withShortWaits(t)()
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	api := &fakeJobAPI{job: &fakeJob{}}
	e := startEngine(t, api, probe, rec.cb())

	// 内层树出现 → running，PID 上报首个内层进程
	probe.set([]uint32{111}, []uint32{111, 112})
	waitState(t, e, StateRunning)
	if snap := e.Snapshot(); snap.PID != 111 || snap.Version != "v1.3.0" {
		t.Errorf("running 快照错误: %+v", snap)
	}

	// 树消失（用户自退）→ stopped（外层退出码不可反映内层，统一归 stopped）
	probe.set(nil, nil)
	waitState(t, e, StateStopped)
}

func TestQuitTerminatesTree(t *testing.T) {
	defer withShortWaits(t)()
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	// Terminate 钩子模拟进程树即刻消失（生产由内核 TerminateJobObject 达成）
	api := &fakeJobAPI{job: &fakeJob{onTerm: func() { probe.set(nil, nil) }}}
	e := startEngine(t, api, probe, rec.cb())
	probe.set([]uint32{111}, []uint32{111})
	waitState(t, e, StateRunning)

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if !api.job.terminated {
		t.Error("Quit 应经 JobObject 终止进程树")
	}
	waitState(t, e, StateStopped)

	// Quit 幂等
	if err := e.Quit(); err != nil {
		t.Errorf("Quit(stopped) = %v, want nil", err)
	}
	if err := e.Stop(); err != nil {
		t.Errorf("Stop(stopped) = %v, want nil", err)
	}
}

func TestQuitDuringStarting(t *testing.T) {
	defer withShortWaits(t)()
	probe := &fakeProbe{}
	api := &fakeJobAPI{job: &fakeJob{}}
	e := startEngine(t, api, probe, Callbacks{}) // 内层迟迟不出现 → 停在 starting
	// Start 返回时状态已同步迁移为 starting（transition 先于 cmd.Start），
	// 立刻 Quit 即落在 startGrace（压缩后 1s）窗口内。勿改回"轮询等待
	// starting"——等待窗口与 startGrace 等长，调度抖动会让引擎抢先判
	// failed（rustdesk 模块 5 跑 4 挂的存量 flaky 同款教训）。
	if got := e.Snapshot().State; got != StateStarting {
		t.Fatalf("Start 后应立即处于 starting，实际 %s", got)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(starting): %v", err)
	}
	waitState(t, e, StateStopped)
}

// TestSpawnWindowExtendsAncestors 派生开窗：新外层登记进 ancestors 闭包后，
// 其派生内层（模拟为新的 own pid）应被继续识别为自有树。
func TestSpawnWindowExtendsAncestors(t *testing.T) {
	defer withShortWaits(t)()
	probe := &fakeProbe{}
	api := &fakeJobAPI{job: &fakeJob{}}
	e := startEngine(t, api, probe, Callbacks{})
	probe.set([]uint32{111}, []uint32{111})
	waitState(t, e, StateRunning)

	if err := e.SpawnWindow(); err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	e.mu.Lock()
	n := len(e.ancestors)
	e.mu.Unlock()
	if n != 2 {
		t.Errorf("ancestors 应含派生外层 PID，实际 %d 个", n)
	}
	probe.setOwn([]uint32{111, 222})
	time.Sleep(200 * time.Millisecond)
	if snap := e.Snapshot(); snap.State != StateRunning {
		t.Errorf("派生树存续期间应保持 running，实际 %s", snap.State)
	}
}

func TestSpawnWindowRequiresRunning(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.SpawnWindow(); err == nil {
		t.Error("静止态派生窗口应报错")
	}
}

// TestRefreshExternalTwoWay 静止态下提取目录出现便携实例 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	probe.all = nil
	e.RefreshExternal()
	if len(rec.states) != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", len(rec.states))
	}

	probe.all = []uint32{333}
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}

	probe.all = nil
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的含自己）
	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()
	probe.all = []uint32{444}
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestRestoreWindowStates 自有/外部唤窗入口的守门行为（真实焦点操作留真机验收）。
func TestRestoreWindowStates(t *testing.T) {
	probe := &fakeProbe{focusN: 2}
	e := NewEngine(&fakeJobAPI{}, probe, Callbacks{})
	if n := e.RestoreWindow(); n != 0 {
		t.Errorf("静止态 RestoreWindow 应返回 0，实际 %d", n)
	}
	if n := e.RestoreExternalWindow(); n != 0 {
		t.Errorf("无外部进程时 RestoreExternalWindow 应返回 0，实际 %d", n)
	}
	probe.all = []uint32{555}
	if n := e.RestoreExternalWindow(); n != 2 {
		t.Errorf("RestoreExternalWindow 应透传焦点计数 %d，实际 %d", 2, n)
	}
}
