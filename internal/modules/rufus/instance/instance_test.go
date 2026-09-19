//go:build windows

package instance

import (
	"errors"
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

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/Quit 的 quitHook+grace 收口）
// 已由内核 packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖
// 真进程）。本文件聚焦 rufus 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// UAC 提权指引覆盖、ErrExternal 契约映射、便携 ini 播种、以及维持既有环境口径
// 的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running    bool // IsRunning 返回值
	ready      bool // WaitForReady 立即返回值
	pids       []uint32
	windowOpen bool // IsMainWindowOpen 返回值
}

func (p *fakeProbe) FindPIDs() []uint32              { return p.pids }
func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsMainWindowOpen([]uint32) bool  { return p.windowOpen }

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

// mustCmdExe 返回系统 cmd.exe 路径（纯路径解析用）。
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

// mustCmdExeCopy 把 cmd.exe 复制到 t.TempDir() 后返回副本路径（真进程冒烟用；
// 必须搭配 specArgs 注入确定性存活命令，裸启动在精简 stdin 环境读 EOF 即退，
// 见 TROUBLESHOOTING #81）。刻意用副本而非系统原件：Start 会向 exe 同目录
// 播种 rufus.ini——原件路径会把种子文件写进 System32。
func mustCmdExeCopy(t *testing.T) string {
	t.Helper()
	src := mustCmdExe(t)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取 cmd.exe 失败: %v", err)
	}
	dst := filepath.Join(t.TempDir(), exeName)
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("复制 cmd.exe 失败: %v", err)
	}
	return dst
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

// ---------- 领域文案与词表映射（纯函数路径） ----------

// TestElevateHint 740 → 文案含「管理员」关键词：前端 ElevateRestart 一键提权
// 按钮显隐的跨层契约（与 bcu 基准一致，文案改写不得丢掉关键词）。
func TestElevateHint(t *testing.T) {
	if got := elevateHint(syscall.Errno(740)); !strings.Contains(got, "管理员") {
		t.Errorf("740 指引文案必须含「管理员」（前端按钮显隐匹配），got %q", got)
	}
	if got := elevateHint(errors.New("no such file")); got != "" {
		t.Errorf("非 740 错误应返回空串走原文案，got %q", got)
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
	// 映射产物必须始终落在 rufus 既有词表内
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

	// 异常退出：内核文案改写回 rufus 既有指引（管理员运行 + 重装建议），退出码入账
	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "v4.15",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "Rufus 异常退出（退出码 1）") || !strings.Contains(snap.Error, "管理员") {
		t.Errorf("异常退出文案应保持 rufus 既有口径: %s", snap.Error)
	}

	// 手动停止：内核"已手动停止"按 rufus 历史口径清空（markeron 金样本相反，
	// 其历史就在传"已手动停止"——映射表刻意分叉，勿"顺手对齐"）
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	snap = rec.last()
	if snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("stopped 映射异常（应清掉内核文案）: %+v", snap)
	}

	// external：External 标志、清 PID（沿用原 setStateExternal 规则）
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	snap = rec.last()
	if !snap.External || snap.State != StateExternal || snap.PID != 0 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// TestElevateOverrideReplacesFailedText spawn 失败提权覆盖：Start 检出 740 后
// errOverride 生效，failed 快照错误文案为管理员指引而非内核原始 PathError。
func TestElevateOverrideReplacesFailedText(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e.mu.Lock()
	e.errOverride = "Rufus 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
	e.mu.Unlock()

	e.onSupState(sup.Snapshot{State: sup.StateFailed, Version: "v4.15",
		Error: "进程启动失败: fork/exec C:\\nowhere\\rufus.exe: 要求提升。"})
	snap := rec.last()
	if snap.Error != e.errOverride {
		t.Errorf("提权覆盖文案未生效: %q", snap.Error)
	}
	if !strings.Contains(snap.Error, "管理员") {
		t.Errorf("覆盖文案必须含「管理员」指引: %q", snap.Error)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v4.15"}); err == nil ||
		err.Error() != "rufus.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExeCopy(t)})
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
	err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExeCopy(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartSeedsIni 冒烟：Start 先于进程创建在 exe 所在目录播种 rufus.ini。
// 用不存在的 exe 路径：Start 必然失败，但 seed 应已落盘（seed 在 spawn 之前）。
func TestStartSeedsIni(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v4.15", Exe: filepath.Join(dir, exeName)}); err == nil {
		t.Fatal("不存在的 exe 应启动失败")
	}
	waitState(t, e, StateFailed)
	data, err := os.ReadFile(filepath.Join(dir, iniFileName))
	if err != nil {
		t.Fatalf("Start 应播种 %s: %v", iniFileName, err)
	}
	if !strings.Contains(string(data), "UpdateCheckInterval = -1") {
		t.Errorf("种子应包含禁用更新检查键: %q", data)
	}
}

// TestSeedPortableSettingsRespectsExisting 播种纪律：已有 rufus.ini（用户配置/
// 导入携带）绝不改写。
func TestSeedPortableSettingsRespectsExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, iniFileName), []byte("Locale = zh-CN"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SeedPortableSettings(dir); err != nil {
		t.Fatalf("SeedPortableSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, iniFileName))
	if err != nil || string(data) != "Locale = zh-CN" {
		t.Errorf("已有配置不得被覆盖: %v %q", err, data)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 以
// 确定性命令拉起 cmd.exe（specArgs 缝注入）→ running → Stop 终止 → stopped
// （错误文案按 rufus 口径为空）。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs

	exe := mustCmdExeCopy(t)
	if err := e.Start(StartOptions{Version: "v4.15", Exe: exe}); err != nil {
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
		t.Errorf("手动停止文案按 rufus 既有口径应为空, got %q", snap.Error)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitGraceFallback 冒烟：Quit 的优雅投递（cmd.exe 无可投顶层窗，等同
// 投递空转）→ 宽限内未退 → JobObject 强杀兜底收口 stopped。quitHook 的
// "宽限内自然退出"优雅路径由内核 engine_test 的 QuitHook 用例矩阵覆盖。
func TestQuitGraceFallback(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExeCopy(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning) // 实例必须在世，Quit 才真正走"宽限未退→强杀兜底"分支
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

// TestStopExternalKeepsNilContract 外部实例不归本引擎管辖：内核回 ErrExternal，
// 按 rufus 既有契约映射为成功无操作，状态校正为 external（在用拒卸/不强杀语义）。
func TestStopExternalKeepsNilContract(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("external 下 Stop 应映射为 nil, got %v", err)
	}
	if snap := e.Snapshot(); snap.State != StateExternal || !snap.External {
		t.Fatalf("external 校正未入账: %+v", snap)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("external 下 Quit 应映射为 nil, got %v", err)
	}
}

// TestRefreshExternalTwoWay 静止态下实例出现 → external，消失 → stopped。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
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

	// running 时不探测（探测到的是自己）：以真进程冒烟验证内核静止态门控
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExeCopy(t)}); err != nil {
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

// TestRestoreWindowNonRunning 非 running 态唤窗是安全 no-op（不 panic 不改状态）。
func TestRestoreWindowNonRunning(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.RestoreWindow()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("no-op 唤窗不应广播，事件数 = %d", got)
	}
	e.RestoreExternalWindow([]uint32{uint32(0)})
}

// TestIsMainWindowOpen running 且有可见窗口信号 → true；非 running → false。
func TestIsMainWindowOpen(t *testing.T) {
	probe := &fakeProbe{windowOpen: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	if e.IsMainWindowOpen() {
		t.Error("stopped 态不应报告窗口打开")
	}
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v4.15", Exe: mustCmdExeCopy(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)
	if !e.IsMainWindowOpen() {
		t.Error("running 态应透传探针结果")
	}
}

// TestExternalPIDsPassthrough 外部 PID 枚举透传（唤窗用）。
func TestExternalPIDsPassthrough(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{pids: []uint32{12, 34}}, Callbacks{})
	pids := e.ExternalPIDs()
	if len(pids) != 2 || pids[0] != 12 || pids[1] != 34 {
		t.Fatalf("ExternalPIDs 透传异常: %v", pids)
	}
}

// TestWaitReadyPassthrough 就绪等待透传探针（轮询语义在 probe 实现侧）。
func TestWaitReadyPassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
}

// TestIsRufusProcess 进程名形态判定：托管名、官方资产名命中；
// hogger（rufus.com）、无关进程、伪装前缀（rufuskiller.exe）绝不混入。
func TestIsRufusProcess(t *testing.T) {
	hits := []string{"rufus.exe", "RUFUS.EXE", "rufus-4.15p.exe", "rufus-4.9.exe", "rufus-4.15_x86.exe"}
	misses := []string{"rufus.com", "rufuskiller.exe", "rufus.txt", "explorer.exe", "rufus-", "rufu.exe"}
	for _, n := range hits {
		if !isRufusProcess(n) {
			t.Errorf("应命中: %s", n)
		}
	}
	for _, n := range misses {
		if isRufusProcess(n) {
			t.Errorf("不应命中: %s", n)
		}
	}
}
