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
// 本文件聚焦 paseo 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、externalSettle 树级静默期的
// 落地条件（仅受管在途探针）、wait 退出分类对"外层主进程退出但 daemon 残树"
// 的 external 接管口径，唤窗双通道接缝，以及维持既有环境口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	mu      sync.Mutex
	running bool // IsRunning 返回值
	ready   bool // WaitForReady 立即返回值
	focus   bool // FocusWindow/IsWindowOpen 返回值
	calls   int  // IsRunning 调用计数
}

func (p *fakeProbe) setRunning(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = v
}

func (p *fakeProbe) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.running
}

func (p *fakeProbe) IsRunning() bool                 { return p.isRunning() }
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

func (r *eventRecorder) statesSince(running int) []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []State
	for _, s := range r.states[running:] {
		out = append(out, s.State)
	}
	return out
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// mustCmdExe 返回系统 cmd.exe 路径（真进程冒烟用）。LookPath 失败时经
// %SystemRoot%\System32 兜底——git-bash 等非系统 PATH 环境下子进程 PATH
// 不含 System32，不能让环境差异掩盖引擎行为断言（既有环境红口径）。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cmd.exe")
	if err == nil {
		return path
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		cand := filepath.Join(root, "System32", "cmd.exe")
		if fi, serr := os.Stat(cand); serr == nil && !fi.IsDir() {
			return cand
		}
	}
	t.Fatal("cmd.exe 不可用")
	return ""
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

// waitRunningEvent 等引擎首个 running 广播出现并返回其下标（供终态事件断言）。
func waitRunningEvent(t *testing.T, rec *eventRecorder) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec.mu.Lock()
		for i, s := range rec.states {
			if s.State == StateRunning {
				rec.mu.Unlock()
				return i
			}
		}
		rec.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("等待 running 广播超时")
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
	// 映射产物必须始终落在 paseo 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, Version: "0.8.0",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "Paseo 异常退出（退出码 1）") || !strings.Contains(snap.Error, "杀毒软件") {
		t.Errorf("异常退出文案应保持 paseo 既有口径: %s", snap.Error)
	}

	// 手动停止：内核"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: manualStopWording})
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
	if err := e.Start(StartOptions{Version: "0.8.0"}); err == nil ||
		err.Error() != "Paseo.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
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
// 不进入 Job 链路）。真实运行链路冒烟由 wait/Quit 系测试覆盖。
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

// TestStopManagedConvergesStopped 冒烟：常驻真进程经 Stop 强杀收口 stopped、
// 文案为空口径保持既有（退出码 0 → stopped 的纯净分类由内核引擎测试矩阵
// 以 fake opener 覆盖，本层不重复造带参数进程的桩）。
func TestStopManagedConvergesStopped(t *testing.T) {
	compressSettle(t)
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", snap.Error)
	}
	if snap := e.Snapshot(); snap.Version != "0.8.0" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
}

// TestWaitOuterExitNoResidualFailed 外层受管主进程被第三方终止（非经引擎
// Stop——那会置 stopping 标记跳过探针）且探针未见进程树残留 → failed，
// 退出码入账且文案保持既有"杀毒软件"指引口径。
func TestWaitOuterExitNoResidualFailed(t *testing.T) {
	compressSettle(t)
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	if p, err := os.FindProcess(int(e.Snapshot().PID)); err == nil {
		_ = p.Kill() // 第三方 TerminateProcess 等价：非零退出码
	}
	waitState(t, e, StateFailed)
	snap := e.Snapshot()
	if snap.ExitCode == 0 {
		t.Errorf("非零退出码应入账，exitCode = %d", snap.ExitCode)
	}
	if !strings.Contains(snap.Error, "Paseo 异常退出（退出码") || !strings.Contains(snap.Error, "杀毒软件") {
		t.Errorf("failed 文案应保持既有口径: %q", snap.Error)
	}
}

// TestWaitOuterExitDaemonTreeResidualTakenExternal 冷启动竞速/残树接管核心场景：
// 外层受管主进程退出（第三方 Kill），进程名探针仍见目标存活（Paseo 形态
// 即内置 daemon/PTY 残树仍挂着 Paseo.exe 镜像）→ 内核 wait 分类 external 接管。
// 断言与迁移前口径一致：收口走 external，绝不落"假 stopped"（整树终止归 Job——
// 收树是 Stop/Terminate 的职责；残树在场时状态机不得谎报已停）。
func TestWaitOuterExitDaemonTreeResidualTakenExternal(t *testing.T) {
	compressSettle(t)
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, rec.cb())
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	ridx := waitRunningEvent(t, rec)

	if p, err := os.FindProcess(int(e.Snapshot().PID)); err == nil {
		_ = p.Kill() // 模拟"外层 Electron 主进程终止、daemon 残树在场"
	}
	waitState(t, e, StateExternal)
	for _, s := range rec.statesSince(ridx + 1) {
		if s == StateStopped {
			t.Fatalf("残树在场不得出现假 stopped，事件序列: %v", rec.statesSince(ridx+1))
		}
	}
	snap := e.Snapshot()
	if !snap.External || snap.PID != 0 {
		t.Fatalf("外部接管快照异常: %+v", snap)
	}
}

// TestInspectSettlesOnlyWhenManaged externalSettle 落地条件：仅受管在途探针
// （wait 退出分类场）先静默再探测，静止态的 RefreshExternal/classify 保持即时。
func TestInspectSettlesOnlyWhenManaged(t *testing.T) {
	old := externalSettle
	externalSettle = 300 * time.Millisecond
	t.Cleanup(func() { externalSettle = old })

	fp := &fakeProbe{}
	e := NewEngine(newWindowsJobAPI(), fp, Callbacks{})
	sp := supProbe{probe: fp, e: e}

	// 静止态：立即返回（远小于 settle）
	start := time.Now()
	_, _, _ = sp.Inspect(nil)
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Fatalf("静止态探针不应静默: %v", d)
	}

	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	// 受管在途：静默满 settle 后才探测
	start = time.Now()
	_, _, _ = sp.Inspect(nil)
	if d := time.Since(start); d < externalSettle {
		t.Fatalf("受管在途探针应先静默 externalSettle: %v < %v", d, externalSettle)
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

	probe.setRunning(true)
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}

	probe.setRunning(false)
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的是自己）：以真进程冒烟验证内核静止态门控
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)
	probe.setRunning(true)
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

// TestQuitGraceFallback 冒烟：Quit 经内核 grace 通道——WM_CLOSE（cmd.exe 无
// Paseo 窗口，postClose 静默 no-op）→ 宽限内未自然退 → JobObject 强杀兜底
// （整树收口），归 stopped 且文案为空。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限与静默期：单测不能等生产 8s/500ms
	oldGrace, oldSettle := closeGracePeriod, externalSettle
	closeGracePeriod = 200 * time.Millisecond
	externalSettle = 10 * time.Millisecond
	defer func() { closeGracePeriod, externalSettle = oldGrace, oldSettle }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "0.8.0", Exe: mustCmdExe(t)}); err != nil {
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
// supervisor 引擎测试矩阵覆盖（fake opener）；本层不重复造带参数进程的桩。

// TestStopIdempotent 停止幂等：静止态调用 Stop/Quit 无副作用。
func TestStopIdempotent(t *testing.T) {
	compressSettle(t)
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 paseo 既有契约映射为成功无操作，状态校正为 external。
func TestStopExternalKeepsNilContract(t *testing.T) {
	compressSettle(t)
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

// TestOpenMessengerFailurePropagates 信使拉起失败 → OpenMessenger 返回既有包装文案。
func TestOpenMessengerFailurePropagates(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.spawnMessenger = func(string) error { return errors.New("拉起窗口信使失败: fake") }
	if err := e.OpenMessenger("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起窗口信使失败") {
		t.Fatalf("信使失败应透传: %v", err)
	}
}

// TestOpenMessenger 信使拉起冒烟：真实 cmd.exe 短命进程（Start 即挂起后由
// Release 交操作系统回收），仅验证不阻塞不报错并广播现态快照。
func TestOpenMessenger(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	if err := e.OpenMessenger(mustCmdExe(t)); err != nil {
		t.Fatalf("OpenMessenger: %v", err)
	}
	if rec.count() == 0 {
		t.Error("信使拉起后应广播一次现态快照")
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
