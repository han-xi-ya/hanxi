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
// 本文件聚焦 everything 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// 运行模式推算（唤窗翻新/竞速接管保留/静止探测清空）、ErrExternal 契约映射、
// "已手动停止"文案折回、信使接缝、-quit 宽限收口，以及维持既有环境口径的
// 真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running    bool // IsEverythingRunning 返回值
	ready      bool // WaitForEverythingReady 立即返回值
	windowOpen bool // IsSearchWindowOpen 返回值（空闲退出豁免信号）
}

func (p *fakeProbe) IsEverythingRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForEverythingReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsSearchWindowOpen() bool                  { return p.windowOpen }

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

// mustCmdExe 返回系统 cmd.exe 路径（受限执行环境经 SystemRoot 兜底定位；
// 仅用于"启动即失败"类路径，不依赖进程驻留）。
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

// newSmokeCmdExe 生成真进程冒烟用的驻留脚本（模拟 Everything 长生命周期 GUI
// 进程）：无参启动（托管主实例语义）挂住 30s 等待 Job/句柄终止；带任意参数
// （窗口信使与 -quit 信使语义）立即退场——不留挂起的信使泄露进程。
// 刻意不用裸 cmd.exe 做驻留桩：其存活性随 stdin 继承环境（控制台 conpty vs
// NUL 设备 EOF）漂移、受限会话里 cmd 检索外部命令又不可靠——同款掷硬币见
// docs/TROUBLESHOOTING.md #81（piclite 迁移实跑），此处按该条标准修复：生命期
// 由命令语义保证（ping 时长），路径一律 SystemRoot 绝对寻径不经检索。
func newSmokeCmdExe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hanxi-everything-smoke.cmd")
	// 驻留命令走 %SystemRoot% 绝对路径：go test 的子 cmd 继承会话 PATH，
	// 受限环境（Git Bash 会话）里裸 ping 不可达会让脚本秒退、冒烟空转。
	content := "@echo off\r\n" +
		"if \"%~1\"==\"\" (\r\n" +
		"  \"%SystemRoot%\\System32\\PING.EXE\" -n 30 127.0.0.1 >nul\r\n" +
		") else (\r\n" +
		"  exit /b 0\r\n" +
		")\r\n"
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
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
	// 映射产物必须始终落在 everything 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "1.5.0.1422b",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "Everything 异常退出（退出码 1）") || !strings.Contains(snap.Error, "索引库") {
		t.Errorf("异常退出文案应保持 everything 既有口径: %s", snap.Error)
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

// TestModeAccounting 运行模式推算三条既有纪律：
//  1. 静止态探测发现外部实例 → 清空不揣测（原 RefreshExternal 规则）；
//  2. 自有进程退出后竞速接管（上一广播态还活着）→ 保留最近模式账目
//     （原 wait external-takeover 分支行为）；
//  3. 自有实例 running 下不再触发清空。
func TestModeAccounting(t *testing.T) {
	// 1. stopped → external：清模式
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e.mu.Lock()
	e.mode = ModeBackground
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	if snap := e.Snapshot(); snap.Mode != "" {
		t.Fatalf("静止态探测外部不应揣测模式, mode = %q", snap.Mode)
	}

	// 2. running → external（竞速接管）：模式保留（广播快照口径——手动注入
	// onSupState 不驱动内核状态机，真实链路里内核状态与广播同源）
	rec2 := &eventRecorder{}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec2.cb())
	e2.onSupState(sup.Snapshot{State: sup.StateStarting, Version: "1.4.1.1032"})
	e2.onSupState(sup.Snapshot{State: sup.StateRunning, Version: "1.4.1.1032"})
	e2.mu.Lock()
	e2.mode = ModeBackground
	e2.mu.Unlock()
	e2.onSupState(sup.Snapshot{State: sup.StateExternal, Version: "1.4.1.1032"})
	if snap := rec2.last(); snap.State != StateExternal || snap.Mode != ModeBackground {
		t.Fatalf("竞速接管应保留最近模式账目: %+v", snap)
	}
	// 接管后模式账目未被静止态清空路径污染
	if snap := rec2.last(); snap.Mode != ModeBackground {
		t.Fatalf("接管广播后模式账目应保持: %+v", snap)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartValidation(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: "illegal"}); err == nil ||
		!strings.Contains(err.Error(), "非法运行模式") {
		t.Fatalf("非法运行模式应被拒绝, got %v", err)
	}
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: "", Mode: ModeWindow}); err == nil ||
		err.Error() != "Everything.exe 路径不能为空" {
		t.Fatalf("空 exe 应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: ModeWindow})
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
	err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: mustCmdExe(t), Mode: ModeWindow})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境口径，经 SystemRoot 兜底定位
// 真实 cmd.exe）：真 Job Object 拉起 → running → OpenWindow 信使（接缝打桩）
// 翻新模式 → Stop 强杀 → stopped。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	var spawned []string
	var spMu sync.Mutex
	e.spawnMessenger = func(exe string) error {
		spMu.Lock()
		spawned = append(spawned, exe)
		spMu.Unlock()
		return nil
	}

	exe := newSmokeCmdExe(t)
	// 用 window 模式（无参）冒烟：background 会追加 -startup 参数——信使脚本带参
	// 立即退场，验证不了驻留语义（-startup 参数映射本身是受控 Spec.Args 直传，
	// 由内核 opener 测试矩阵负责）。
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: exe, Mode: ModeWindow}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	if got := e.Snapshot().Mode; got != ModeWindow {
		t.Errorf("窗口启动模式账目 = %q, want %q", got, ModeWindow)
	}
	e.mu.Lock()
	e.mode = ModeBackground // 复位为后台态，验证唤窗翻新
	e.mu.Unlock()
	if got := e.Exe(); got != exe {
		t.Errorf("running 时 Exe() = %q, want %q", got, exe)
	}
	// Windows 单调钟粒度可致"注册即查询"同 tick（时长 0）——给一个观察宽限再断言
	time.Sleep(5 * time.Millisecond)
	if e.RunningDuration() <= 0 {
		t.Error("running 时已运行时长应为正")
	}

	opened, err := e.OpenWindow(exe)
	if err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
	spMu.Lock()
	spawnCount := len(spawned)
	spMu.Unlock()
	if spawnCount != 1 || spawned[0] != exe {
		t.Errorf("唤窗应拉起一个信使: %v", spawned)
	}
	if snap := rec.last(); snap.State != StateRunning || snap.Mode != ModeWindow {
		t.Errorf("自有实例唤窗后应保持 running 并翻新模式为 window: %+v", snap)
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	snap := e.Snapshot()
	if snap.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", snap.Error)
	}
	if snap.Version != "1.5.0.1422b" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitGraceFallback 冒烟：Quit 经内核 grace 通道——-quit 信使真实拉起
// （cmd.exe 不识别 -quit，立即报错退场，等同"信使无效"场景）→ 主进程赖着
// 不走 → 宽限内未自然收口 → JobObject 强杀兜底，收口归 stopped。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 5s
	old := quitGracePeriod
	quitGracePeriod = 300 * time.Millisecond
	defer func() { quitGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: newSmokeCmdExe(t), Mode: ModeWindow}); err != nil {
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
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 everything 既有契约映射为成功无操作，状态校正为 external。
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

// TestRefreshExternalTwoWay 静止态下实例出现 → external（模式清空不揣测），
// 消失 → stopped；running 态不受影响。
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
	if got := e.Snapshot().Mode; got != "" {
		t.Fatalf("外部实例不应揣测运行模式, mode = %q", got)
	}

	probe.running = false
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测（探测到的是自己或并存通道实例）：以真进程冒烟验证内核静止态门控
	if err := e.Start(StartOptions{Version: "1.5.0.1422b", Exe: newSmokeCmdExe(t), Mode: ModeWindow}); err != nil {
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

// TestOpenWindowMessengerFailurePropagates 信使拉起失败 → OpenWindow 返回既有包装文案。
func TestOpenWindowMessengerFailurePropagates(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.spawnMessenger = func(string) error { return errors.New("拉起窗口信使失败: fake") }
	if opened, err := e.OpenWindow("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起窗口信使失败") || opened {
		t.Fatalf("信使失败应透传: opened=%v err=%v", opened, err)
	}
}

// TestOpenWindowMessenger 信使拉起冒烟：真实 cmd.exe（-startup/-quit 类未知参数
// 立即报错退场），仅验证不阻塞不报错。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t)); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady / IsSearchWindowOpen 不经内核）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true, windowOpen: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	if !e.IsSearchWindowOpen() {
		t.Fatal("fake 探针 windowOpen=true 应透传为窗口可见")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false, windowOpen: false}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
	if e2.IsSearchWindowOpen() {
		t.Fatal("fake 探针 windowOpen=false 应透传为窗口不可见")
	}
}
