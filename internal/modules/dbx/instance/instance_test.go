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

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/收口等待/env 注入透传）已由
// 内核 packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖
// 真进程）。本文件聚焦 dbx 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、数据改道 env 注入组装、
// 信使接缝（含接管场 dataDir 透传）、WM_CLOSE 宽限收口，以及维持既有环境
// 口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running    bool // IsRunning 返回值
	ready      bool // WaitForReady 立即返回值
	windowOpen bool // IsMainWindowOpen 返回值
	pids       []uint32
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.ready }
func (p *fakeProbe) IsMainWindowOpen() bool          { return p.windowOpen }
func (p *fakeProbe) FindPIDs() []uint32              { return p.pids }

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
	// 映射产物必须始终落在 dbx 既有词表内
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

	e.onSupState(sup.Snapshot{State: sup.StateFailed, PID: 0, Version: "v1.5.3",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "DBX 异常退出（退出码 1）") || !strings.Contains(snap.Error, "WebView2") {
		t.Errorf("异常退出文案应保持 dbx 既有口径: %s", snap.Error)
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

// TestSupProbeInspectCarriesPID 探针适配：拒探兜底命中进程枚举时，Inspect
// 附带首个 DBX.exe PID（external 快照据此充实）；纯互斥体命中（枚举不可得）
// 只报存活、ProcInfo 留 nil，沿用 ccswitch 口径。
func TestSupProbeInspectCarriesPID(t *testing.T) {
	alive, info, err := supProbe{&fakeProbe{running: true, pids: []uint32{4242}}}.Inspect(nil)
	if !alive || err != nil {
		t.Fatalf("Inspect = %v, %v; want true, nil", alive, err)
	}
	if info == nil || info.PID != 4242 || info.Name != exeImageName {
		t.Fatalf("external PID 未入账: %+v", info)
	}
	alive, info, _ = supProbe{&fakeProbe{running: true}}.Inspect(nil)
	if !alive || info != nil {
		t.Fatalf("无 PID 可报时应仅报存活: alive=%v info=%+v", alive, info)
	}
	if alive, _, _ = (supProbe{&fakeProbe{running: false}}).Inspect(nil); alive {
		t.Fatal("探针判不在场时 Inspect 必须报 false")
	}
}

// TestStartInjectsDataDirEnv 数据改道注入面（纯函数钉死）：DataDir 非空 →
// 内核 Spec.Env 得 "DBX_DATA_DIR=<path>"；空 → nil（纯继承）。
func TestStartInjectsDataDirEnv(t *testing.T) {
	env := dataDirEnv(`E:\HanxiData\dbx`)
	if len(env) != 1 || env[0] != "DBX_DATA_DIR=E:\\HanxiData\\dbx" {
		t.Fatalf("注入 env 异常: %v", env)
	}
	if got := dataDirEnv(""); got != nil {
		t.Fatalf("空 DataDir 应不注入: %v", got)
	}
	if EnvDataDirKey != "DBX_DATA_DIR" {
		t.Fatalf("阶段 0 裁定的 env 键名被改动: %s", EnvDataDirKey)
	}
}

// ---------- 生命周期（假 Job：Start 失败路径；真 Job：正例冒烟） ----------

func TestStartEmptyExeRejected(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "v1.5.3"}); err == nil ||
		err.Error() != "DBX.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	err := e.Start(StartOptions{Version: "v1.5.3", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "v1.5.3", Exe: mustCmdExe(t), DataDir: `X:\fixture\dbx-data`})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟（既有环境红口径）：真 Job Object 以
// 确定性命令拉起 cmd.exe（specArgs 缝注入，携 DataDir 走注入通道）→
// running → OpenWindow 信使（接缝打桩，断言 dataDir 透传）→ Stop 强杀 → stopped。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeCmdArgs
	type msg struct{ exe, dataDir string }
	var spawned []msg
	var spMu sync.Mutex
	e.spawnMessenger = func(exe, dataDir string) error {
		spMu.Lock()
		spawned = append(spawned, msg{exe, dataDir})
		spMu.Unlock()
		return nil
	}

	exe := mustCmdExe(t)
	if err := e.Start(StartOptions{Version: "v1.5.3", Exe: exe, DataDir: `X:\fixture\dbx-data`}); err != nil {
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

	opened, err := e.OpenWindow(exe, `X:\fixture\dbx-data`)
	if err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
	spMu.Lock()
	spawnCount := len(spawned)
	spMu.Unlock()
	if spawnCount != 1 || spawned[0].exe != exe || spawned[0].dataDir != `X:\fixture\dbx-data` {
		t.Errorf("唤窗应拉起一个携带受控数据目录的信使: %v", spawned)
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
	if snap.Version != "v1.5.3" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestQuitGraceFallback 冒烟：Quit 经内核 grace 通道——WM_CLOSE（cmd.exe 替身
// 无标题 "DBX" 主窗，静默 no-op）→ 宽限内未自然退 → JobObject 强杀兜底，
// 收口归 stopped。
func TestQuitGraceFallback(t *testing.T) {
	// 压缩宽限：单测不能等生产 2s
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.specArgs = smokeCmdArgs
	if err := e.Start(StartOptions{Version: "v1.5.3", Exe: mustCmdExe(t)}); err != nil {
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
// 按 dbx 既有契约映射为成功无操作，状态校正为 external。
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

// TestRefreshExternalTwoWay 静止态下互斥体（探针 IsRunning）出现 → external，
// 消失 → stopped。Quit 后"托管备份"worker 残留正是经此通道如实呈现。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())

	// stopped 且判据不在场 → 无变化不广播
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
	if err := e.Start(StartOptions{Version: "v1.5.3", Exe: mustCmdExe(t)}); err != nil {
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
	e.spawnMessenger = func(string, string) error { return errors.New("拉起窗口信使失败: fake") }
	if opened, err := e.OpenWindow("whatever.exe", ""); err == nil ||
		!strings.Contains(err.Error(), "拉起窗口信使失败") || opened {
		t.Fatalf("信使失败应透传: opened=%v err=%v", opened, err)
	}
}

// TestOpenWindowMessenger 信使拉起冒烟：真实 cmd.exe 短命进程（裸启读 EOF 即退），
// 携 DataDir 走 env 注入分支，仅验证不阻塞不报错。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t), `X:\fixture\dbx-data`); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}

// TestProbePassthrough 探针领域能力透传（WaitReady / IsMainWindowOpen 不经内核）。
func TestProbePassthrough(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: true, windowOpen: true}, Callbacks{})
	if !e.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=true 应透传为就绪")
	}
	if !e.IsMainWindowOpen() {
		t.Fatal("fake 探针 windowOpen=true 应透传为窗口可见")
	}
	e2 := NewEngine(&fakeJobAPI{}, &fakeProbe{ready: false, windowOpen: false}, Callbacks{})
	if e2.WaitReady(time.Second) {
		t.Fatal("fake 探针 ready=false 应透传为超时")
	}
	if e2.IsMainWindowOpen() {
		t.Fatal("fake 探针 windowOpen=false 应透传为窗口不可见")
	}
}

// TestMutexNameContract 探针领域常量钉死（阶段 0 实证面：改一个字母都算回归）：
// identifier com.dbx.app + 插件后缀 -sim/-sic/-siw，无 semver 后缀。
func TestMutexNameContract(t *testing.T) {
	cases := map[string]string{
		"mutexName":       mutexName,
		"msgWindowClass":  msgWindowClass,
		"msgWindowName":   msgWindowName,
		"exeImageName":    exeImageName,
		"mainWindowTitle": mainWindowTitle,
	}
	want := map[string]string{
		"mutexName":       "com.dbx.app-sim",
		"msgWindowClass":  "com.dbx.app-sic",
		"msgWindowName":   "com.dbx.app-siw",
		"exeImageName":    "DBX.exe",
		"mainWindowTitle": "DBX",
	}
	for k, v := range cases {
		if v != want[k] {
			t.Errorf("%s = %q, want %q", k, v, want[k])
		}
	}
}
