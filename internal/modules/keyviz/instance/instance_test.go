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
// 本文件聚焦 keyviz 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、无优雅通道的 Quit 直接强杀
// 语义、Detach → SetAllowKillOnClose(false) 接线，以及维持既有环境口径的
// 真进程冒烟。

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

// recordingJobAPI 包装真 JobAPI：透传全部操作并记录 SetAllowKillOnClose 调用，
// 断言 Detached → SetAllowKillOnClose(false) 接线（真终止语义不受影响）。
type recordingJobAPI struct {
	inner platform.JobAPI
	mu    sync.Mutex
	calls []bool
}

func (r *recordingJobAPI) Create() (platform.Job, error) {
	j, err := r.inner.Create()
	if err != nil {
		return nil, err
	}
	return &recordingJob{Job: j, api: r}, nil
}

type recordingJob struct {
	platform.Job
	api *recordingJobAPI
}

func (r *recordingJob) SetAllowKillOnClose(b bool) error {
	r.api.mu.Lock()
	r.api.calls = append(r.api.calls, b)
	r.api.mu.Unlock()
	return r.Job.SetAllowKillOnClose(b)
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
	// 映射产物必须始终落在 keyviz 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "v2.1.1",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "Keyviz 异常退出（退出码 1）") || !strings.Contains(snap.Error, "WebView2") {
		t.Errorf("异常退出文案应保持 keyviz 既有口径: %s", snap.Error)
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
	if err := e.Start(StartOptions{Version: "v2.1.1"}); err == nil ||
		err.Error() != "keyviz.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v2.1.1", Exe: mustCmdExe(t)})
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
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v2.1.1", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartDetachedWiring Detached=true 应接线内核 Spec.DetachFromJob →
// SetAllowKillOnClose(false)（"不随 Hanxi 关闭"开关，现状保持迁移不漂移）。
func TestStartDetachedWiring(t *testing.T) {
	api := &recordingJobAPI{inner: newWindowsJobAPI()}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v2.1.1", Exe: mustCmdExe(t), Detached: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	api.mu.Lock()
	calls := append([]bool(nil), api.calls...)
	api.mu.Unlock()
	if len(calls) != 1 || calls[0] != false {
		t.Fatalf("Detached=true 应恰好一次 SetAllowKillOnClose(false)，got %v", calls)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 以
// 确定性命令拉起 cmd.exe（specArgs 缝注入）→ running → Stop 强杀 → stopped。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	exe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "v2.1.1", Exe: exe}); err != nil {
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

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	snap := e.Snapshot()
	if snap.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", snap.Error)
	}
	if snap.Version != "v2.1.1" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitDirectKill Keyviz 无优雅通道：Quit 的 QuitHook 即时返错 → 内核跳过
// grace 窗口直接强杀常驻进程；即便互斥体探针"仍在"（外部误判源），stopping
// 标记令退出分类归 stopped 而非 external/failed。并钉住"不耗 grace 预算"
// （钩子返错后不得静等 quitGraceWindow）。
func TestQuitDirectKill(t *testing.T) {
	probe := &fakeProbe{running: true} // 若无 stopping 标记会被误判 external
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v2.1.1", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)

	begin := time.Now()
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	if elapsed := time.Since(begin); elapsed >= quitGraceWindow {
		t.Errorf("Quit 应即时强杀不耗 grace 预算，实际耗时 %v", elapsed)
	}
	if snap := e.Snapshot(); snap.Error != "" || snap.External {
		t.Errorf("Quit 收口应为无文案 stopped: %+v", snap)
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

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 keyviz 既有契约映射为成功无操作，状态校正为 external。
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

	// running 时不探测（探测到的是自己）：以真进程冒烟验证内核静止态门控
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v2.1.1", Exe: mustCmdExe(t)}); err != nil {
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

// TestProbePassthrough 探针领域能力透传（WaitReady 不经内核，service 冷启动
// 就绪等待与 external 轮询共用）。
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
