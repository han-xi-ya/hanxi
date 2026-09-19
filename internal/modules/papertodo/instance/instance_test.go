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

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/grace 收口等待）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖真进程）。
// 本文件聚焦 papertodo 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、show/hide/exit 命令信使接缝
// （含 QuitHook 收编验证），以及维持既有环境口径的真进程冒烟。

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

// captureMessenger 注入命令信使接缝，记录 exe+args 调用，避免测试真实拉起进程。
func captureMessenger(t *testing.T, e *Engine) *[]string {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	e.launchMessenger = func(exe string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, exe+" "+strings.Join(args, " "))
		return nil
	}
	return &calls
}

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
	// 映射产物必须始终落在 papertodo 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, Version: "v3.31",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	// 既有领域文案：no-runtime 变体的 .NET 10 桌面运行时指引不许被内核文案吞掉
	if !strings.Contains(snap.Error, "PaperTodo 异常退出（退出码 1）") ||
		!strings.HasSuffix(snap.Error, "（环境检测页可查看）") {
		t.Errorf("异常退出文案应保持 papertodo 既有口径: %s", snap.Error)
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

	// external：External 标志、清 PID（沿用原 setStateExternal 规则）
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	snap = rec.last()
	if !snap.External || snap.State != StateExternal || snap.PID != 0 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v3.31"}); err == nil ||
		err.Error() != "PaperTodo.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
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

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 拉起
// cmd.exe → running → show/hide 信使（接缝打桩不真拉进程）→ Stop 强杀 → stopped。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	calls := captureMessenger(t, e)

	exe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "v3.31", Exe: exe}); err != nil {
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

	if err := e.OpenWindow(exe); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if err := e.HidePapers(exe); err != nil {
		t.Fatalf("HidePapers: %v", err)
	}
	if got := *calls; len(got) != 2 ||
		!strings.HasSuffix(got[0], " show") || !strings.HasSuffix(got[1], " hide") {
		t.Errorf("唤窗/收拢应各发一条 show/hide 命令信使: %v", got)
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
	if snap.Version != "v3.31" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitSendsExitMessengerViaGrace Quit 经内核 grace 通道：QuitHook 恰派一条
// exit 命令信使；信使后自有进程（模拟"主实例收到命令 → 保存后自退"）在宽限内
// 消失 → 收口 stopped、手动停止文案折回空口径。
func TestQuitSendsExitMessengerViaGrace(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 3 * time.Second
	defer func() { closeGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	cmdExe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "v3.31", Exe: cmdExe}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	pid := e.Snapshot().PID
	if pid == 0 {
		t.Fatal("running 快照应持有自有 PID")
	}

	// 注入信使：记录命令，并模拟"主实例收到 exit → 保存后自退"——
	// 后台终止自有进程让内核 wait 分类收口（Quit 已置 stopping → stopped）。
	var calls []string
	var mu sync.Mutex
	e.launchMessenger = func(exe string, args ...string) error {
		mu.Lock()
		calls = append(calls, strings.Join(args, " "))
		mu.Unlock()
		go func() {
			time.Sleep(100 * time.Millisecond)
			if p, perr := os.FindProcess(int(pid)); perr == nil {
				_ = p.Kill()
			}
		}()
		return nil
	}

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("优雅收口文案应为空口径, got %q", snap.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "exit" {
		t.Errorf("Quit 应恰发一条 exit 命令信使，实际 %v", calls)
	}
}

// TestQuitGraceFallback 主实例对 exit 无响应（进程不退出）→ 宽限超时后
// JobObject 强杀兜底，收口归 stopped 且不带文案。
func TestQuitGraceFallback(t *testing.T) {
	old := closeGracePeriod
	defer func() { closeGracePeriod = old }()
	closeGracePeriod = 200 * time.Millisecond

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	var calls []string
	var mu sync.Mutex
	e.launchMessenger = func(exe string, args ...string) error {
		mu.Lock()
		calls = append(calls, strings.Join(args, " "))
		mu.Unlock()
		return nil // 吞掉：信使"发出去了"但主实例不理会（进程不退）
	}
	if err := e.Start(StartOptions{Version: "v3.31", Exe: mustCmdExe(t)}); err != nil {
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
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "exit" {
		t.Errorf("Quit 应先派 exit 信使再走强杀兜底，实际 %v", calls)
	}
}

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 papertodo 既有契约映射为成功无操作，状态校正为 external。
func TestStopExternalKeepsNilContract(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("external 下 Quit 应映射为 nil, got %v", err)
	}
	if snap := e.Snapshot(); snap.State != StateExternal || !snap.External {
		t.Fatalf("external 校正未入账: %+v", snap)
	}
	e2 := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e2.Stop(); err != nil {
		t.Fatalf("external 下 Stop 应映射为 nil, got %v", err)
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

	// running 时不探测（探测到的正是自己）：以真进程冒烟验证内核静止态门控
	if err := e.Start(StartOptions{Version: "v3.31", Exe: mustCmdExe(t)}); err != nil {
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

// TestOpenWindowMessengerFailurePropagates 信使拉起失败 → 既有包装文案透传，
// 且 OpenWindow/HidePapers 的领域前缀不混用。
func TestOpenWindowMessengerFailurePropagates(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.launchMessenger = func(string, ...string) error { return errors.New("fake") }
	if err := e.OpenWindow("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起窗口信使失败") {
		t.Fatalf("唤窗信使失败应透传既有文案: %v", err)
	}
	if err := e.HidePapers("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起收拢信使失败") {
		t.Fatalf("收拢信使失败应透传既有文案: %v", err)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady 不经内核）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
}
