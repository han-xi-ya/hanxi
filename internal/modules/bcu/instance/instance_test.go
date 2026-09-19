//go:build windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	sup "hanxi/packages/go/supervisor"
)

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/收口等待，含冷启动竞速的
// external-takeover 分类）已由内核 packages/go/supervisor 的引擎测试矩阵覆盖
// （fake opener，全路径不依赖真进程；"自有退场但探针仍见目标→external"用例
// 见 engine_test.go）。本文件聚焦 bcu 适配层：内核快照 → 本包 Snapshot 的
// 形状/词表映射、740 提权指引覆盖、ErrExternal 契约映射、信使接缝、
// WM_CLOSE 宽限收口，以及维持既有环境口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running    bool // IsRunning 返回值
	ready      bool // WaitForReady 立即返回值
	windowOpen bool // IsMainWindowOpen 返回值（空闲退出豁免信号）

	mu         sync.Mutex
	lastWinPID uint32 // IsMainWindowOpen 最近收到的 PID 参数（锚点传导断言）
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsMainWindowOpen(pid uint32) bool {
	p.mu.Lock()
	p.lastWinPID = pid
	p.mu.Unlock()
	return p.windowOpen
}

func (p *fakeProbe) seenWindowPID() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastWinPID
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

// mustCmdExe 返回系统 cmd.exe 路径（真进程冒烟用；无参数启动即挂起等待输入，
// 由 Job/句柄负责终止；短命用例显式 /c exit N）。
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

// ---------- 长命辅助子进程（生命周期冒烟专用，确定性真进程） ----------

// smokeHelperEnv TestMain 握手变量：值非空时本测试二进制以"长命辅助子进程"
// 形态运行（睡死等 Job/句柄终止，不跑任何用例、无窗口、不读 stdin）。
// 选择此形态而非裸 cmd.exe：go test 的 stdin 是管道（EOF 即时），无参 cmd.exe
// 会在数毫秒内退 0（沙箱实跑实证），running 态断言将随调度抖动机翻红；
// 辅助子进程是真实 PE、走同一 CreateProcess/Job 绑定/终止链路，只多不漏。
const smokeHelperEnv = "BCU_INSTANCE_SMOKE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(smokeHelperEnv) != "" {
		time.Sleep(10 * time.Minute) // 睡死等终止（time.Sleep 挂定时器，不触发死锁探测器）
		return
	}
	os.Exit(m.Run())
}

// mustSmokeHelper 返回"以辅助子进程形态再运行本测试二进制"的路径：
// 环境变量经 t.Setenv 注入并由 Start 继承（supervisor Spec.Env 空=纯继承）。
func mustSmokeHelper(t *testing.T) string {
	t.Helper()
	t.Setenv(smokeHelperEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("定位测试二进制失败: %v", err)
	}
	return exe
}

// ---------- 领域文案与词表映射（纯函数路径） ----------

// TestElevateHint BCU manifest 为 requireAdministrator：未提权父进程得到裸
// errno 740（exec 层可能带 %w 包装），特判输出管理员指引；其余错误返回空串
// 走原文案。
func TestElevateHint(t *testing.T) {
	got := elevateHint(syscall.Errno(740))
	if got == "" {
		t.Fatal("740 应输出提权指引文案")
	}
	// 跨层契约：前端 ElevateRestart 组件以"文案含'管理员'"判定一键提权重启
	// 按钮显隐（BCUView needsElevate），文案改写时不得丢掉该关键词。
	if !strings.Contains(got, "管理员") {
		t.Errorf("指引文案必须含「管理员」关键词（前端按钮显隐匹配），got %q", got)
	}
	if got := elevateHint(fmt.Errorf("fork/exec: %w", syscall.Errno(740))); got == "" {
		t.Fatal("包装后的 740 也应被识别")
	}
	// 内核包装层（supervisor 在 spawn 错误外加"进程启动失败: %w"）同样穿透识别
	if got := elevateHint(fmt.Errorf("进程启动失败: %w", fmt.Errorf("fork/exec: %w", syscall.Errno(740)))); got == "" {
		t.Fatal("内核包装后的 740 也应被识别")
	}
	if got := elevateHint(errors.New("no such file")); got != "" {
		t.Fatalf("非 740 错误应返回空串，got %q", got)
	}
}

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
	// 映射产物必须始终落在 bcu 既有词表内
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

	// 异常退出：内核文案改写回 bcu 既有指引（重装建议），退出码入账
	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "6.2.0",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if want := "BCU 异常退出（退出码 1）。请尝试重新安装该版本"; snap.Error != want {
		t.Errorf("异常退出文案应保持 bcu 既有口径: got %q want %q", snap.Error, want)
	}

	// 手动停止：内核"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	snap = rec.last()
	if snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("stopped 映射异常（Error 应折回空文案）: %+v", snap)
	}

	// 启动失败等其余文案透传
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "进程创建失败: fake"})
	if got := rec.last().Error; got != "进程创建失败: fake" {
		t.Errorf("非异常退出文案应透传: %s", got)
	}

	// external：External 标志、清 PID（沿用原 setStateExternal 规则）
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	snap = rec.last()
	if !snap.External || snap.State != StateExternal || snap.PID != 0 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// TestElevateOverrideReplacesFailedText spawn 失败提权覆盖：errOverride 在场时
// failed 快照错误文案为管理员指引而非内核原始 PathError（事件契约保持
// "failed 即带管理员指引"，前端 needsElevate 判定的跨层依赖）。
func TestElevateOverrideReplacesFailedText(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e.mu.Lock()
	e.errOverride = elevateHint(syscall.Errno(740))
	e.mu.Unlock()

	e.onSupState(sup.Snapshot{State: sup.StateFailed, Version: "6.2.0",
		Error: "进程启动失败: fork/exec C:\\nowhere\\BCUninstaller.exe: 要求操作已提升。"})
	snap := rec.last()
	if snap.Error != e.errOverride {
		t.Errorf("提权覆盖文案未生效: %q", snap.Error)
	}
	if !strings.Contains(snap.Error, "管理员") {
		t.Errorf("覆盖文案必须含「管理员」指引: %q", snap.Error)
	}
	// 提权覆盖只作用于 failed：异常退出码映射在覆盖缺席时仍生效
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e2.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 2）"})
	if snap := rec.last(); snap.ExitCode != 2 || !strings.Contains(snap.Error, "重新安装") {
		t.Errorf("无覆盖时异常退出文案口径异常: %+v", snap)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0"}); err == nil ||
		err.Error() != "BCUninstaller.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustCmdExe(t)}); err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartAssignFail(t *testing.T) {
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustCmdExe(t)}); err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（长命辅助子进程，全环境确定性）：真
// Job Object 拉起 → running → OpenWindow 信使（接缝打桩）→ Stop 强杀 → stopped。
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

	exe := mustSmokeHelper(t)
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: exe}); err != nil {
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
	if snap.Version != "6.2.0" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitGraceFallback 冒烟：Quit 经内核 grace 通道——WM_CLOSE（辅助子进程
// 无顶层窗口，投递空转）→ 宽限内未自然退 → JobObject 强杀兜底，收口归
// stopped。quitHook 的"宽限内自然退出"优雅路径由内核 engine_test 的 QuitHook
// 用例矩阵覆盖；本层不重复造带参数进程的桩。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustSmokeHelper(t)}); err != nil {
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

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 bcu 既有契约映射为成功无操作，状态校正为 external（service 层给指引）。
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
	if err := e.Start(StartOptions{Version: "6.2.0", Exe: mustSmokeHelper(t)}); err != nil {
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

// ---------- 信使与探针透传 ----------

// TestOpenWindowMessengerFailurePropagates 信使拉起失败 → OpenWindow 返回既有包装文案。
func TestOpenWindowMessengerFailurePropagates(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.spawnMessenger = func(string) error { return errors.New("拉起窗口信使失败: fake") }
	if opened, err := e.OpenWindow("whatever.exe"); err == nil ||
		!strings.Contains(err.Error(), "拉起窗口信使失败") || opened {
		t.Fatalf("信使失败应透传: opened=%v err=%v", opened, err)
	}
}

// TestOpenWindowMessengerElevation740 外部实例常由用户以管理员自启：未提权的
// Hanxi 拉起信使被 740 直拒时，返回管理员指引而非英文裸错误（与 Start 路径
// 口径一致，前端 ElevateRestart 依赖"管理员"关键词）。
func TestOpenWindowMessengerElevation740(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.spawnMessenger = func(string) error {
		return fmt.Errorf("拉起窗口信使失败: %w", fmt.Errorf("fork/exec: %w", syscall.Errno(740)))
	}
	opened, err := e.OpenWindow("C:\\somewhere\\BCUninstaller.exe")
	if opened || err == nil || !strings.Contains(err.Error(), "管理员") {
		t.Fatalf("740 应给出管理员指引, opened=%v err=%v", opened, err)
	}
}

// TestOpenWindowMessenger 信使拉起冒烟：真实 cmd.exe 短命进程，仅验证不阻塞不报错。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t)); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady 不经内核；IsMainWindowOpen
// 仅 running 态以自有 PID 向探针传递）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false, windowOpen: true}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
	if e2.IsMainWindowOpen() {
		t.Fatal("stopped 状态应恒为 false")
	}

	probe := &fakeProbe{windowOpen: true}
	e3 := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	if err := e3.Start(StartOptions{Version: "6.2.0", Exe: mustSmokeHelper(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e3.Stop() }()
	waitState(t, e3, StateRunning)
	if !e3.IsMainWindowOpen() {
		t.Fatal("running + probe 可见时应为 true")
	}
	if pid := probe.seenWindowPID(); pid == 0 || pid != e3.Snapshot().PID {
		t.Errorf("窗口探测应按自有实例 PID 传导: got %d, want %d", pid, e3.Snapshot().PID)
	}
	probe.windowOpen = false
	if e3.IsMainWindowOpen() {
		t.Fatal("probe 不可见时应为 false")
	}
}
