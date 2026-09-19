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

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

func (r *eventRecorder) last() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[len(r.states)-1]
}

func newWindowsJobAPI() platform.JobAPI { return windows.NewJobAPI() }

// mustCmdExe 系统 cmd.exe 路径（真进程冒烟用）。个别受限执行环境（如 PATH 精简
// 的沙箱）System32 不在 PATH 中：经 SystemRoot 定位兜底，仍是真实系统 cmd.exe，
// 不改变冒烟口径。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("cmd.exe"); err == nil {
		return path
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "cmd.exe")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	t.Fatal("cmd.exe 不可用")
	return ""
}

// smokeLoopArgs 真机冒烟存活命令：裸 cmd.exe 在精简 stdin 环境下读 EOF 即退，
// 不得用于冒烟——注入纯 CPU 循环让替身进程确定性存活数秒（Stop/defer 收口
// 杀掉），断言不再依赖时钟毫秒粒度（translucenttb 同款定式）。
var smokeLoopArgs = []string{"/c", `for /l %i in (1,1,1500000) do @set x=%i`}

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

func TestStartEmptyExe(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	err := e.Start(StartOptions{Form: "portable"})
	if err == nil || err.Error() != "Code.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartCreateJobFail(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: mustCmdExe(t)})
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
	err := e.Start(StartOptions{Version: "1.136.1", Form: "installer", Exe: mustCmdExe(t)})
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestManagedLifecycleSmoke 真进程冒烟：真 Job Object 拉起 cmd.exe（specArgs 注入
// 确定性存活命令）→ running → OpenWindow 信使（接缝打桩）→ Quit 优雅窗口超时
// 强杀兜底 → stopped；手动停止话术按既有口径折回空文案。
func TestManagedLifecycleSmoke(t *testing.T) {
	old := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = old }()

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

	exe := mustCmdExe(t)
	e.specArgs = smokeLoopArgs
	if err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: exe}); err != nil {
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
	if snap := e.Snapshot(); snap.Form != "portable" || snap.Version != "1.136.1" {
		t.Errorf("形态/版本账目应随快照携带: %+v", snap)
	}

	if opened, err := e.OpenWindow(exe); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
	spMu.Lock()
	spawnCount := len(spawned)
	spMu.Unlock()
	if spawnCount != 1 || spawned[0] != exe {
		t.Errorf("唤窗应拉起一个信使: %v", spawned)
	}
	if snap := rec.last(); snap.State != StateRunning {
		t.Errorf("信使广播快照应保持 running: %+v", snap)
	}

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	snap := e.Snapshot()
	if snap.Error != "" {
		t.Errorf("手动停止文案应保持既有空口径, got %q", snap.Error)
	}
	if snap.Version != "1.136.1" {
		t.Errorf("停止后应保留版本账目: %+v", snap)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// TestStopForceSmoke Stop 强杀通道冒烟：不投 WM_CLOSE 不耗宽限，直接收口 stopped。
func TestStopForceSmoke(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	e.specArgs = smokeLoopArgs
	if err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("手动停止文案应折回空口径, got %q", snap.Error)
	}
}

// TestWaitAbnormalExitSmoke 真进程异常退出冒烟：退出码 1 且探针未见存活 →
// failed，ExitCode 入账、话术逐字还原（内核分类 + 本层映射全链）。
func TestWaitAbnormalExitSmoke(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	// 借内核 Args 让 cmd.exe 立即以退出码 1 收场（生产 Start 无参拉起 Code.exe）
	if err := e.sup.Start(context.Background(), sup.Spec{Version: "1.136.1", Exe: mustCmdExe(t),
		Args: []string{"/c", "exit", "1"}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateFailed)
	snap := e.Snapshot()
	if snap.ExitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", snap.ExitCode)
	}
	want := "VS Code 异常退出（退出码 1）。请确认托管文件完整未被杀软隔离，或重启 Hanxi 后重试"
	if snap.Error != want {
		t.Errorf("异常退出话术应逐字还原, got %q", snap.Error)
	}
}

// TestWaitExternalTaken 冷启动竞速冒烟：我方进程信使化自退（exit 0）后探测信号
// 仍在 → external（外部主实例接管，本引擎不冒领归属）。
func TestWaitExternalTaken(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e.sup.Start(context.Background(), sup.Spec{Version: "1.136.1", Exe: mustCmdExe(t),
		Args: []string{"/c", "exit", "0"}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateExternal)
	if snap := e.Snapshot(); !snap.External || snap.PID != 0 {
		t.Fatalf("快照异常: %+v", snap)
	}
}

// TestWaitExitCodeZeroClassifiedStopped 进程正常退出（关窗即退）→ stopped。
func TestWaitExitCodeZeroClassifiedStopped(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false}, Callbacks{})
	if err := e.sup.Start(context.Background(), sup.Spec{Version: "1.136.1", Exe: mustCmdExe(t),
		Args: []string{"/c", "exit", "0"}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateStopped)
	if snap := e.Snapshot(); snap.Error != "" {
		t.Errorf("关窗即退应保持空话术口径, got %q", snap.Error)
	}
}

// TestRefreshExternalTwoWay 静止态下探测信号出现 → external，消失 → stopped；
// running 时不探测（探测到的正是自己）。
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

	probe.running = false
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}

	// running 时不探测：起一个确定性存活实例再校正，状态不得被改写
	e.specArgs = smokeLoopArgs
	if err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: mustCmdExe(t)}); err != nil {
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

// TestQuitExternalNoOp external 态 Quit/Stop 不越权（内核回 ErrExternal，
// 按既有契约映射为无操作成功，指引文案由 service 层给出）。
func TestQuitExternalNoOp(t *testing.T) {
	probe := &fakeProbe{running: true}
	e := NewEngine(newWindowsJobAPI(), probe, Callbacks{})
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(external) = %v, want nil", err)
	}
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(external) = %v, want nil", err)
	}
}

func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestOpenWindowMessenger 信使真实拉起冒烟：cmd.exe 短命进程（无参即退），
// 仅验证不阻塞不报错、错误话术接缝在位。
func TestOpenWindowMessenger(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if opened, err := e.OpenWindow(mustCmdExe(t)); err != nil || !opened {
		t.Fatalf("OpenWindow = %v, %v; want true, nil", opened, err)
	}
}

// TestStoppingFoldedIntoRunning 词表折并冒烟：终止窗口内 stopping 中间态对前端
// 保持 running 语义（本包既有词表无 stopping），收口后由终态广播纠正。
func TestStoppingFoldedIntoRunning(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, rec.cb())
	e.specArgs = smokeLoopArgs
	if err := e.Start(StartOptions{Version: "1.136.1", Form: "portable", Exe: mustCmdExe(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e.Stop() }()
	waitState(t, e, StateRunning)

	// 强杀收口（grace=0 不投 WM_CLOSE）：事件流不得出现词表外状态
	old := closeGracePeriod
	closeGracePeriod = 100 * time.Millisecond
	defer func() { closeGracePeriod = old }()
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, s := range rec.states {
		switch s.State {
		case StateStarting, StateRunning, StateStopped, StateExternal, StateFailed:
		default:
			t.Fatalf("事件流出现词表外状态 %q", s.State)
		}
	}
}

// ---------- 便携版路径前缀匹配边界 ----------

func TestIsUnderDir(t *testing.T) {
	root := filepath.Join("D:", "hanxi", "versions")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "vscode_1.136.1", "Code.exe"), true},
		{filepath.Join(root, "Code.exe"), true},
		// 前缀相似但非目录边界：D:\hanxi\versions-evil 不得命中 D:\hanxi\versions
		{"D:\\hanxi\\versions-evil\\Code.exe", false},
		{root, false},                                   // 目录自身不算其内部
		{"D:\\vscode\\Code.exe", false},                 // 安装版目录不误入便携前缀
		{strings.ToUpper(root) + "\\X\\Code.exe", true}, // Windows 路径大小写不敏感
	}
	for _, c := range cases {
		if got := isUnderDir(c.path, root); got != c.want {
			t.Errorf("isUnderDir(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
