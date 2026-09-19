//go:build windows

package instance

import (
	"context"
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
	sup "hanxi/packages/go/supervisor"
)

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/收口等待）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖真进程）。
// 本文件聚焦 flclash 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、Inspect 适配器的 PID 充实、
// 信使接缝、唤窗直操作与 WM_CLOSE 宽限收口，以及维持既有环境口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	mu         sync.Mutex
	pids       []uint32 // FindPIDs/IsRunning/Inspect 的事实源
	ready      bool     // WaitForReady 立即返回值
	windowOpen bool     // IsMainWindowOpen 返回值（空闲退出豁免信号）
}

func (p *fakeProbe) FindPIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.pids...)
}
func (p *fakeProbe) IsRunning() bool { return len(p.FindPIDs()) > 0 }
func (p *fakeProbe) WaitForReady(time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}
func (p *fakeProbe) IsMainWindowOpen(pids []uint32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.windowOpen && len(pids) > 0
}
func (p *fakeProbe) setPids(pids []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pids = pids
}

type fakeJob struct{ assignErr error }

func (f *fakeJob) Assign(pid uint32) error        { return f.assignErr }
func (f *fakeJob) Close() error                   { return nil }
func (f *fakeJob) Terminate(code uint32) error    { return nil }
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

// mustCmdExe 返回系统 cmd.exe 路径（真进程冒烟的替身载体；必须搭配 specArgs
// 注入确定性存活命令，裸启动在精简 stdin 环境读 EOF 即退，见 TROUBLESHOOTING #81）。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("cmd.exe"); err == nil {
		return path
	}
	// 受限执行环境(如 Git Bash 会话)PATH 不含 System32：经 SystemRoot 定位兜底，
	// 仍是真实系统 cmd.exe，不改变冒烟口径。
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "cmd.exe")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	t.Fatal("cmd.exe 不可用")
	return ""
}

// smokeCmdArgs 真进程冒烟的确定性命令（specArgs 缝注入）：裸 cmd.exe 在精简
// stdin 环境下读 EOF 即退，存活时长不可控——纯 CPU 循环让替身进程稳定存活
// 数秒（Stop/defer 收口杀掉），断言不再依赖时钟毫秒粒度。
var smokeCmdArgs = []string{"/c", `for /l %i in (1,1,1500000) do @set x=%i`}

// waitState 轮询直至引擎达到目标状态（或超时）。
func waitState(t *testing.T, e *Engine, want State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
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

// ---------- 词表与形状映射（纯函数路径） ----------

func TestMapStateVocabulary(t *testing.T) {
	cases := []struct {
		in   sup.State
		want State
	}{
		{sup.StateStopped, StateStopped},
		{sup.StateStarting, StateStarting},
		{sup.StateRunning, StateRunning},
		{sup.StateStopping, StateRunning}, // 终止窗口折并入 running（词表无 stopping）
		{sup.StateExternal, StateExternal},
		{sup.StateFailed, StateFailed},
	}
	for _, c := range cases {
		if got := mapState(c.in); got != c.want {
			t.Errorf("mapState(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	// 映射产物必须始终落在 flclash 既有词表内
	for _, c := range cases {
		switch mapState(c.in) {
		case StateStopped, StateStarting, StateRunning, StateFailed, StateExternal:
		default:
			t.Fatalf("映射出词表之外的状态: %s", mapState(c.in))
		}
	}
}

func TestExitCodeAndMessageMapping(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())

	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "0.8.96",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if snap.Error != "FlClash 异常退出（退出码 1）。请尝试重新安装该版本" {
		t.Errorf("异常退出文案应保持 flclash 既有口径逐字一致: %s", snap.Error)
	}

	// 手动停止：内核"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	snap = rec.last()
	if snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("stopped 映射异常（Error 应折回空文案）: %+v", snap)
	}

	// 启动失败等其余文案透传
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "进程启动失败: fake"})
	if got := rec.last().Error; got != "进程启动失败: fake" {
		t.Errorf("非异常退出文案应透传: %s", got)
	}

	// external：External 标志 + Inspect 充实的存活 PID（flclash 探针可给出
	// PID，快照如实携带——与互斥体探针族"external 恒 PID 0"的差异点）
	e.onSupState(sup.Snapshot{State: sup.StateExternal, PID: 777})
	snap = rec.last()
	if !snap.External || snap.State != StateExternal || snap.PID != 777 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// ---------- Inspect 形状适配（进程枚举 → 内核探针契约） ----------

func TestSupProbeInspectOwnPidInfo(t *testing.T) {
	probe := &fakeProbe{}
	// 无进程：running=false 且无 ProcInfo
	if running, info, err := (supProbe{probe}).Inspect(context.Background()); running || info != nil || err != nil {
		t.Fatalf("空快照应报 not running: %v %+v %v", running, info, err)
	}
	// 有进程：running=true，附带首个命中 PID 与进程名
	probe.setPids([]uint32{4242, 99})
	running, info, err := (supProbe{probe}).Inspect(context.Background())
	if err != nil || !running || info == nil || info.PID != 4242 || info.Name != exeName {
		t.Fatalf("Inspect 应返回 (true, ownPidInfo): %v %+v %v", running, info, err)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "0.8.96"}); err == nil ||
		err.Error() != "FlClash.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "0.8.96", Exe: mustCmdExe(t)}); err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "0.8.96", Exe: mustCmdExe(t)}); err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 以
// 确定性命令拉起 cmd.exe（specArgs 缝注入）→ running → RestoreWindow 直操作
// （cmd.exe 无窗口，静默 no-op 但应重播 running 快照）→ Stop 强杀 → stopped。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs

	exe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "0.8.96", Exe: exe}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	if got := e.Exe(); got != exe {
		t.Errorf("running 时 Exe() = %q, want %q", got, exe)
	}
	// Windows 单调钟粒度可致"注册即查询"同 tick（时长 0）——给一个观察宽限再断言
	time.Sleep(5 * time.Millisecond)
	if e.RunningDuration() <= 0 {
		t.Error("running 时已运行时长应为正")
	}

	// 唤窗直操作：无窗口进程上 no-op 但必须重播当前快照（既有 RestoreWindow 语义）
	before := rec.count()
	e.RestoreWindow()
	if rec.count() <= before {
		t.Error("RestoreWindow(running) 应重播快照")
	}
	if snap := rec.last(); snap.State != StateRunning {
		t.Errorf("唤窗后广播快照应保持 running: %+v", snap)
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	snap := e.Snapshot()
	if snap.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", snap.Error)
	}
	if snap.Version != "0.8.96" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestRestoreWindowStoppedNoop 静止态唤窗：不广播（与既有实现一致）。
func TestRestoreWindowStoppedNoop(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.RestoreWindow()
	if rec.count() != 0 {
		t.Errorf("stopped 态 RestoreWindow 不应广播, events = %d", rec.count())
	}
}

// TestRestoreExternalWindowAndPIDs 外部唤窗路径：ExternalPIDs 透传探针枚举，
// RestoreExternalWindow 对其逐个执行直操作（无窗口进程上 no-op 不报错）。
func TestRestoreExternalWindowAndPIDs(t *testing.T) {
	probe := &fakeProbe{pids: []uint32{1, 2, 3}}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	if got := e.ExternalPIDs(); len(got) != 3 {
		t.Fatalf("ExternalPIDs 应透传探针结果, got %v", got)
	}
	e.RestoreExternalWindow(e.ExternalPIDs()) // 不 panic、不报错即达成（Win32 尽力而为）
}

// TestSingleInstanceCheckSeam 信使路径保留接缝：spawnMessenger 默认接真实
// spawn，测试注入替换——印证"信使只触发单实例检查"的能力留在模块、
// 生产唤窗不依赖它（详见 messenger.go 的语义论证）。
func TestSingleInstanceCheckSeam(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if e.spawnMessenger == nil {
		t.Fatal("信使接缝应默认接真实 spawnMessenger")
	}
	var got string
	e.spawnMessenger = func(exe string) error { got = exe; return nil }
	if err := e.triggerSingleInstanceCheck("FlClash.exe"); err != nil {
		t.Fatalf("triggerSingleInstanceCheck: %v", err)
	}
	if got != "FlClash.exe" {
		t.Errorf("接缝应转发 exe, got %q", got)
	}
	e.spawnMessenger = func(string) error { return errors.New("拉起单实例检查信使失败: fake") }
	if err := e.triggerSingleInstanceCheck("x"); err == nil || !strings.Contains(err.Error(), "信使") {
		t.Fatalf("信使失败应透传, got %v", err)
	}
}

// TestQuitGraceFallback 冒烟：Quit 经内核 grace 通道——WM_CLOSE（cmd.exe 无
// 窗口，静默 no-op）→ 宽限内未自然退 → JobObject 强杀兜底，收口归 stopped。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "0.8.96", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("Quit 收口文案应为空, got %q", snap.Error)
	}
}

// 注：宽限窗口内自然退出的优雅路径（QuitHook 投递成功 → done 先收口）由内核
// supervisor 引擎测试矩阵覆盖（fake opener）；本层不重复造带窗口的桩。

// TestStopIdempotent 停止幂等：静止态调用 Stop/Quit 无副作用。
func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 flclash 既有契约映射为成功无操作，状态校正为 external 且 PID 由探针充实。
func TestStopExternalKeepsNilContract(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{pids: []uint32{777}}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("external 下 Quit 应映射为 nil, got %v", err)
	}
	snap := e.Snapshot()
	if snap.State != StateExternal || !snap.External || snap.PID != 777 {
		t.Fatalf("external 校正未入账: %+v", snap)
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{pids: []uint32{777}}, Callbacks{})
	if err := e2.Stop(); err != nil {
		t.Fatalf("external 下 Stop 应映射为 nil, got %v", err)
	}
}

// TestRefreshExternalTwoWay 静止态下进程出现 → external（PID 充实），消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	// stopped 且进程不在 → 无变化不广播
	e.RefreshExternal()
	if got := rec.count(); got != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", got)
	}

	probe.setPids([]uint32{999})
	e.RefreshExternal()
	snap := e.Snapshot()
	if snap.State != StateExternal || !snap.External || snap.PID != 999 {
		t.Fatalf("state = %+v, want external(pid 999)", snap)
	}

	probe.setPids(nil)
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的是自己）：以真进程冒烟验证内核静止态门控
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "0.8.96", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)
	probe.setPids([]uint32{999})
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady / IsMainWindowOpen 不经内核）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true, windowOpen: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	// IsMainWindowOpen 仅自有 running 态向探针传 pid
	if e.IsMainWindowOpen() {
		t.Fatal("stopped 态窗口豁免信号应恒为 false")
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{ready: false, windowOpen: true}, Callbacks{})
	if e2.WaitReady(time.Millisecond) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
	e2.specArgs = smokeCmdArgs
	if err := e2.Start(StartOptions{Version: "0.8.96", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e2.Stop() }()
	waitState(t, e2, StateRunning)
	if !e2.IsMainWindowOpen() {
		t.Fatal("running 态应透传探针的窗口可见判定")
	}
}
