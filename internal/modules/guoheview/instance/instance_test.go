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
	sup "hanxi/packages/go/supervisor"
)

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/收口等待）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖真进程）。
// 本文件聚焦 guoheview 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、多实例收敛三大红线
// （唤窗按自有 PID、WM_CLOSE 按自有 PID、独立窗口信使不经内核），
// 以及维持既有环境口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running   bool // IsRunning / RunningBesides 返回值
	ready     bool // WaitForReady 立即返回值
	focusOK   bool // FocusMainWindow 返回值
	focusAny  bool // FocusAnyWindow 返回值
	lastFocus uint32
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) RunningBesides(uint32) bool      { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) FocusMainWindow(pid uint32) bool {
	p.lastFocus = pid
	return p.focusOK
}
func (p *fakeProbe) FocusAnyWindow() bool { return p.focusAny }

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

// mustCmdExe 返回系统 cmd.exe 路径（真进程冒烟用；无参数启动即挂起等待输入，
// 由 Job/句柄负责终止）。
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
	// 映射产物必须始终落在 guoheview 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "v3.2.7.98",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "果核看图异常退出（退出码 1）") || !strings.Contains(snap.Error, "版本管理重新安装") {
		t.Errorf("异常退出文案应保持 guoheview 既有口径: %s", snap.Error)
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

	// external：External 标志、PID 保持探针口径（supProbe 不产出 ProcInfo → 0，
	// 沿用原 setStateExternal 规则）
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	snap = rec.last()
	if !snap.External || snap.State != StateExternal || snap.PID != 0 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v3.2.7.98"}); err == nil ||
		err.Error() != "GuoheView.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
	// 既有契约：Job 创建失败文案透传呈现（"创建 Job Object 失败: …"）
	if msg := e.Snapshot().Error; !strings.Contains(msg, "创建 Job Object 失败") {
		t.Errorf("失败文案应保留 Job 创建错误口径: %q", msg)
	}
}

func TestStartAssignFail(t *testing.T) {
	rec := &eventRecorder{}
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 以
// 确定性命令拉起 cmd.exe → running → Stop 强杀 → stopped；全程账目口径
// （版本保留/Exe 清空/手动停止空文案/已运行时长）逐项核对。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs

	exe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: exe}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	snap := e.Snapshot()
	if snap.PID == 0 || snap.Version != "v3.2.7.98" {
		t.Fatalf("running 快照异常: %+v", snap)
	}
	if got := e.Exe(); got != exe {
		t.Errorf("running 时 Exe() = %q, want %q", got, exe)
	}
	// Windows 单调钟粒度可致"注册即查询"同 tick（时长 0）——给一个观察宽限再断言
	time.Sleep(5 * time.Millisecond)
	if e.RunningDuration() <= 0 {
		t.Error("running 时已运行时长应为正")
	}
	if rec.last().State != StateRunning {
		t.Errorf("广播快照应保持 running: %+v", rec.last())
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	final := e.Snapshot()
	if final.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", final.Error)
	}
	if final.Version != "v3.2.7.98" {
		t.Errorf("停止后应保留版本账目: %+v", final)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestFocusTargetsOwnPid 唤窗红线：只按引擎自有 PID 聚焦（多实例上游误伤
// 用户窗是事故）。真进程 running 态下 Focus 应以快照 PID 命中探针；
// 非 running 态恒 false 且不触发探针。
func TestFocusTargetsOwnPid(t *testing.T) {
	probe := &fakeProbe{focusOK: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})

	if e.Focus() {
		t.Fatal("stopped 态应恒 false")
	}
	if probe.lastFocus != 0 {
		t.Fatalf("stopped 态不应调用探针, lastFocus = %d", probe.lastFocus)
	}

	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	if !e.Focus() {
		t.Fatal("running + focusOK 应为 true")
	}
	if pid := e.Snapshot().PID; probe.lastFocus != pid || pid == 0 {
		t.Fatalf("探针应按自有 PID 调用: lastFocus = %d, snapshot PID = %d", probe.lastFocus, pid)
	}
}

// TestQuitHookPostsOwnPidAndForceKillFallback 冒烟 + 红线：Quit 经内核 grace
// 通道，QuitHook 按自有 PID 投递 WM_CLOSE（接缝注入断言投递目标，cmd.exe
// 替身无果核窗口，真实投递为静默 no-op）→ 宽限内未自然退 → JobObject 强杀
// 兜底，收口归 stopped 且文案折回空。
func TestQuitHookPostsOwnPidAndForceKillFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	// WM_CLOSE 投递接缝打桩：记录目标 PID，不触碰真实窗口
	var closePids []uint32
	var cpMu sync.Mutex
	oldPost := postCloseFn
	postCloseFn = func(pid uint32) {
		cpMu.Lock()
		closePids = append(closePids, pid)
		cpMu.Unlock()
	}
	defer func() { postCloseFn = oldPost }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)
	ownPID := e.Snapshot().PID

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)

	cpMu.Lock()
	pids := append([]uint32(nil), closePids...)
	cpMu.Unlock()
	if len(pids) != 1 || pids[0] != ownPID || ownPID == 0 {
		t.Fatalf("WM_CLOSE 应恰好按自有 PID 投递一次: pids = %v, ownPID = %d", pids, ownPID)
	}
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("Quit 收口文案应为空, got %q", snap.Error)
	}
}

// 注：宽限窗口内自然退出的优雅路径（QuitHook 投递成功 → done 先收口）由内核
// supervisor 引擎测试矩阵覆盖（fake opener）；本层不重复造带参数进程的桩。

// TestLaunchDetachedWindowSeam 独立窗口信使：经接缝断言拉起目标与错误透传；
// 该通道刻意不经内核（不进 Job、不 Wait），见 LaunchDetachedWindow 注释。
func TestLaunchDetachedWindowSeam(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	var launched []string
	e.launchDetached = func(exe string) error {
		launched = append(launched, exe)
		return nil
	}
	if err := e.LaunchDetachedWindow(mustCmdExe(t)); err != nil {
		t.Fatalf("LaunchDetachedWindow: %v", err)
	}
	if len(launched) != 1 || launched[0] != mustCmdExe(t) {
		t.Errorf("应拉起独立窗口一次: %v", launched)
	}
	if got := e.Snapshot().State; got != StateStopped {
		t.Errorf("独立窗口不得进托管生命周期账目, state = %s", got)
	}

	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e2.launchDetached = func(string) error { return errors.New("拉起看图窗口失败: fake") }
	if err := e2.LaunchDetachedWindow("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起看图窗口失败") {
		t.Fatalf("拉起失败应透传既有文案, got %v", err)
	}
}

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

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖（用户自行打开的
// 窗口归用户）：内核回 ErrExternal，按 guoheview 既有契约映射为成功无操作，
// 状态校正为 external 且快照 PID 保持 0 原口径（指引文案由 service 层给出）。
func TestStopExternalKeepsNilContract(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("external 下 Quit 应映射为 nil, got %v", err)
	}
	if snap := e.Snapshot(); snap.State != StateExternal || !snap.External || snap.PID != 0 {
		t.Fatalf("external 校正未入账或 PID 口径漂移: %+v", snap)
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e2.Stop(); err != nil {
		t.Fatalf("external 下 Stop 应映射为 nil, got %v", err)
	}
}

// TestRefreshExternalTwoWay 静止态下外部进程出现 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	// stopped 且无外部进程 → 无变化不广播
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

	// running 时不探测（探测到的可能是自己）：以真进程冒烟验证内核静止态门控
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v3.2.7.98", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)
	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady / FocusAnyWindow 不经内核）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true, focusAny: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	if !e.FocusExternal() {
		t.Fatal("fake 探针 focusAny=true 应透传为唤回成功")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false, focusAny: false}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
	if e2.FocusExternal() {
		t.Fatal("fake 探针 focusAny=false 应透传为唤回失败")
	}
}
