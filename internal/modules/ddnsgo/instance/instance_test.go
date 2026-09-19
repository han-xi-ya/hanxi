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

// 说明：进程治理主流程（spawn/Job 绑定/就绪与退出分类/收口等待）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖（fake opener，全路径不依赖真进程）。
// 本文件聚焦 ddnsgo 适配层：Spec 扩展字段透传（HideWindow/Env/ReadyTimeout）、
// 端口预检与双通道探针语义、内核快照 → 本包 Snapshot 的词表/文案映射、
// OnLog 脱敏管线、Quit/Stop 双通道幂等，以及维持既有环境口径的真进程冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	running bool // IsRunning 返回值（ddns-go.exe 进程扫描）
	port    bool // PortOpen 返回值（web 端口就绪）
}

func (p *fakeProbe) FindPIDs() []uint32 {
	if p.running {
		return []uint32{1}
	}
	return nil
}
func (p *fakeProbe) IsRunning() bool      { return p.running }
func (p *fakeProbe) PortOpen(string) bool { return p.port }

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

// eventRecorder 记录状态/日志广播，便于断言事件序列。
type eventRecorder struct {
	mu      sync.Mutex
	states  []Snapshot
	entries []LogEntry
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{
		OnState: func(s Snapshot) {
			r.mu.Lock()
			r.states = append(r.states, s)
			r.mu.Unlock()
		},
		OnLog: func(e LogEntry) {
			r.mu.Lock()
			r.entries = append(r.entries, e)
			r.mu.Unlock()
		},
	}
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

// mustCmdExe 返回系统 cmd.exe 路径（真进程冒烟用）。个别受限执行环境（如
// PATH 精简的沙箱）System32 不在 PATH 中：经 SystemRoot 定位兜底，
// 仍是真实系统 cmd.exe，不改变冒烟口径。
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

func startOpts(exe string) StartOptions {
	return StartOptions{Version: "v6.17.6", Exe: exe, ListenAddr: "127.0.0.1:9876"}
}

// ---------- Spec 扩展字段透传（本样本的验证目标） ----------

// TestSpecPassthrough 断言本层向内核提交的 Spec 完整携带 ddnsgo 所需字段：
// HideWindow（替代旧 child_windows 蓝本）、Env 后门注入、-l 参数、
// 端口就绪 ReadyTimeout、Detached 开关联动。seam 打桩不触真进程。
func TestSpecPassthrough(t *testing.T) {
	probe := &fakeProbe{}
	e := NewEngine(&fakeJobAPI{}, probe, Callbacks{})
	var got sup.Spec
	var calls int
	e.supStart = func(_ context.Context, spec sup.Spec) error {
		got = spec
		calls++
		return nil
	}

	if err := e.Start(startOpts(`C:\fake\ddns-go.exe`)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if calls != 1 {
		t.Fatalf("应委托内核一次, calls = %d", calls)
	}
	if !got.HideWindow {
		t.Error("HideWindow 应为 true（控制台子系统程序无窗拉起）")
	}
	if len(got.Env) != 1 || got.Env[0] != "DDNS_GO_DAEMON=1" {
		t.Errorf("Env 应恰含上游后门变量, got %v", got.Env)
	}
	if strings.Join(got.Args, " ") != "-l 127.0.0.1:9876" {
		t.Errorf("Args = %v, want [-l 127.0.0.1:9876]", got.Args)
	}
	if got.ReadyTimeout != readyTimeout {
		t.Errorf("ReadyTimeout = %v, want %v", got.ReadyTimeout, readyTimeout)
	}
	if got.Version != "v6.17.6" || got.Exe != `C:\fake\ddns-go.exe` || got.DetachFromJob {
		t.Errorf("基础字段透传异常: %+v", got)
	}

	// Detached 开关联动 Spec
	e.supStart = func(_ context.Context, spec sup.Spec) error { got = spec; return nil }
	probe.port, probe.running = false, false
	if err := e.Start(StartOptions{Version: "v6.17.6", Exe: `C:\fake\ddns-go.exe`, ListenAddr: "127.0.0.1:9876", Detached: true}); err != nil {
		t.Fatalf("Start(detached): %v", err)
	}
	if !got.DetachFromJob {
		t.Error("Detached=true 应联动 Spec.DetachFromJob")
	}
}

// ---------- 端口预检（模块自持，内核 Spec 无启动前挂点） ----------

// TestStartPortOccupiedByExternal 端口预检：端口被占 + 存在 ddns-go.exe 进程
// → 判外部实例接管（内核 external 入账），不创建子进程。
func TestStartPortOccupiedByExternal(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: true, port: true}
	spawned := false
	e := NewEngine(&fakeJobAPI{}, probe, rec.cb())
	e.supStart = func(context.Context, sup.Spec) error { spawned = true; return nil }

	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil || !strings.Contains(err.Error(), "非 Hanxi 托管") {
		t.Fatalf("外部实例占位应返回指引错误, got %v", err)
	}
	if spawned {
		t.Fatal("external 预检拦截后不得拉起进程")
	}
	snap := e.Snapshot()
	if snap.State != StateExternal || !snap.External {
		t.Fatalf("state = %s, want external", snap.State)
	}
}

// TestStartPortOccupiedByOther 端口被非 ddns-go 程序占用 → failed 且文案指向
// 更换端口；failed 为模块叠加态（内核静止 stopped），任何内核非静止广播撤销。
func TestStartPortOccupiedByOther(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{running: false, port: true}
	spawned := false
	e := NewEngine(&fakeJobAPI{}, probe, rec.cb())
	e.supStart = func(context.Context, sup.Spec) error { spawned = true; return nil }

	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("端口占用应返回错误, got %v", err)
	}
	if spawned {
		t.Fatal("预检失败不应拉起进程")
	}
	snap := e.Snapshot()
	if snap.State != StateFailed || !strings.Contains(snap.Error, "其他程序占用") {
		t.Fatalf("应落 failed 且文案指向端口: %+v", snap)
	}
	if snap.PID != 0 {
		t.Fatal("预检失败不应有 PID")
	}

	// 外部 ddns-go 随后出现：内核 external 入账，叠加 failed 撤销
	probe.running = true
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}
	// 外部消失 → stopped，错误文案不得回滚为叠加的端口占用文案
	probe.running = false
	e.RefreshExternal()
	snap = e.Snapshot()
	if snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("叠加 failed 应随内核 external→stopped 迁移撤销: %+v", snap)
	}
}

// ---------- Start 内核失败路径（真 spawn + 假 Job） ----------

func TestStartCreateJobFail(t *testing.T) {
	e := NewEngine(&fakeJobAPI{createErr: errors.New("boom")}, &fakeProbe{}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	snap := e.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("state = %s, want failed", snap.State)
	}
	if !strings.Contains(snap.Error, "创建 Job Object 失败") {
		t.Errorf("failed 文案应指向 Job 创建: %q", snap.Error)
	}
}

func TestStartAssignFail(t *testing.T) {
	api := &fakeJobAPI{job: &fakeJob{assignErr: errors.New("boom")}}
	e := NewEngine(api, &fakeProbe{}, Callbacks{})
	err := e.Start(startOpts(mustCmdExe(t)))
	if err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// TestStartReadyAbortRewritesMessage 就绪等待负路径：端口永不可用 + 进程早退
// （模拟上游端口冲突/异常环境）→ 内核主动终止落 failed，本层把内核"未就绪"
// 文案改写回 ddnsgo 既有口径（快照与返回值双通道）。
func TestStartReadyAbortRewritesMessage(t *testing.T) {
	oldTimeout := readyTimeout
	readyTimeout = 2 * time.Second
	defer func() { readyTimeout = oldTimeout }()

	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: false, port: false}, rec.cb())
	err := e.Start(startOpts(mustCmdExe(t))) // cmd.exe 带 -l 参数秒退
	if err == nil {
		t.Fatal("就绪失败应返回错误")
	}
	if !strings.Contains(err.Error(), "始终不可用") {
		t.Errorf("返回错误应为既有文案口径: %v", err)
	}
	snap := e.Snapshot()
	if snap.State != StateFailed || !strings.Contains(snap.Error, "始终不可用") {
		t.Fatalf("应落 failed 且文案既有口径: %+v", snap)
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
}

func TestExitCodeAndMessageMapping(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e.mu.Lock()
	e.listen = "127.0.0.1:9876"
	e.mu.Unlock()

	// 内核异常退出 → ddnsgo 既有文案 + 退出码入账
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Version: "v6.17.6",
		Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateFailed || snap.ExitCode != 1 {
		t.Fatalf("failed 映射异常: %+v", snap)
	}
	if !strings.Contains(snap.Error, "ddns-go 异常退出（退出码 1）") ||
		!strings.Contains(snap.Error, "端口 127.0.0.1:9876") {
		t.Errorf("异常退出文案应保持 ddnsgo 既有口径: %s", snap.Error)
	}

	// 内核就绪失败 → 改写既有"端口始终不可用"文案
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "启动后实例在 20s 内未就绪，已终止"})
	if snap = rec.last(); !strings.Contains(snap.Error, "始终不可用") {
		t.Errorf("就绪失败文案未改写: %s", snap.Error)
	}

	// 手动停止：ddnsgo 旧口径从不外发"已手动停止"，映射清空
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	if snap = rec.last(); snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("stopped 映射异常: %+v", snap)
	}

	// external：External 标志、清 PID
	e.onSupState(sup.Snapshot{State: sup.StateExternal})
	if snap = rec.last(); !snap.External || snap.State != StateExternal || snap.PID != 0 {
		t.Fatalf("external 映射异常: %+v", snap)
	}
}

// ---------- 日志管线（内核 OnLog → 脱敏 → 环形缓冲 → 事件） ----------

func TestOnSupLogScrubAndBuffer(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, rec.cb())
	e.onSupLog("请求失败 https://api.dnspod.com?login_token=abc123&Domain=xx")

	if got := e.Logs(1); len(got) != 1 || !strings.Contains(got[0], "login_token=***") {
		t.Fatalf("环形缓冲应存脱敏行: %v", got)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.entries) != 1 || !strings.Contains(rec.entries[0].Line, "login_token=***") ||
		strings.HasSuffix(rec.entries[0].Line, "\n") {
		t.Fatalf("事件应携带脱敏且无行尾换行的行: %+v", rec.entries)
	}
}

// ---------- 真进程冒烟（既有环境红口径）：running → Quit 终止 → stopped ----------

// TestManagedLifecycleSmoke 以打桩 Spec（cmd.exe 长眠参数替换 -l 探测参数，
// 保留 HideWindow/Env 等其余透传字段真 spawn）走内核全链路：端口 fake 就绪
// → running → Exe/ListenAddr/RunningDuration 账目 → Quit（quitHook 静默期
// 打桩跳过 + 强制终止）→ stopped 且文案清空。
func TestManagedLifecycleSmoke(t *testing.T) {
	oldQuiet, oldMax := configSettleQuiet, configSettleMax
	configSettleQuiet, configSettleMax = 0, 0 // 跳过真实家目录配置静默等待（非本用例主题）
	defer func() { configSettleQuiet, configSettleMax = oldQuiet, oldMax }()

	rec := &eventRecorder{}
	probe := &fakeProbe{}
	e := NewEngine(newWindowsJobAPI(), probe, rec.cb())
	e.supStart = func(ctx context.Context, spec sup.Spec) error {
		// 换成长眠参数使真进程可被观察与终止；其余字段原样透传内核。
		// 端口就绪在预检放行后才翻转（模拟"起进程→web 端口起来"时序）。
		spec.Args = []string{"/c", "ping", "-n", "30", "127.0.0.1"}
		spec.ReadyTimeout = 3 * time.Second
		probe.port = true
		return e.sup.Start(ctx, spec)
	}

	exe := mustCmdExe(t)
	if err := e.Start(startOpts(exe)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitState(t, e, StateRunning)
	if got := e.Exe(); got != exe {
		t.Errorf("running 时 Exe() = %q, want %q", got, exe)
	}
	if got := e.ListenAddr(); got != "127.0.0.1:9876" {
		t.Errorf("running 时 ListenAddr() = %q", got)
	}
	time.Sleep(5 * time.Millisecond) // Windows 单调钟粒度宽限
	if e.RunningDuration() <= 0 {
		t.Error("running 时已运行时长应为正")
	}

	if err := e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitState(t, e, StateStopped)
	snap := e.Snapshot()
	if snap.Error != "" {
		t.Errorf("手动退出文案应为空（ddnsgo 既有口径）: %q", snap.Error)
	}
	if snap.ListenAddr != "127.0.0.1:9876" {
		t.Errorf("ListenAddr 为快照常显字段（旧口径），退出后仍应保留: %q", snap.ListenAddr)
	}
	if got := e.Exe(); got != "" {
		t.Errorf("停止后 Exe() 应为空, got %q", got)
	}
	if got := e.ListenAddr(); got != "" {
		t.Errorf("停止后 ListenAddr() 应为空, got %q", got)
	}
	if e.RunningDuration() != 0 {
		t.Error("停止后已运行时长应为 0")
	}
}

// ---------- Quit / Stop 双通道与幂等 ----------

// TestStopIdempotent 静止态停止无副作用（内核幂等收口）。
func TestStopIdempotent(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{}, Callbacks{})
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit(stopped) = %v, want nil", err)
	}
}

// TestQuitExternalKeepsNilContract external 态不归本引擎管辖：内核回
// ErrExternal，按 ddnsgo 既有契约映射为无操作成功并校正状态。
func TestQuitExternalKeepsNilContract(t *testing.T) {
	e := NewEngine(newWindowsJobAPI(), &fakeProbe{running: true}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("external 下 Quit 应映射为 nil, got %v", err)
	}
	if snap := e.Snapshot(); snap.State != StateExternal || !snap.External {
		t.Fatalf("external 校正未入账: %+v", snap)
	}
}

// TestRefreshExternalTwoWay 静止态下进程扫描命中 → external，消失 → stopped；
// running 态不探测（探测到的是自己）。
func TestRefreshExternalTwoWay(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
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

	// running 时不探测：真进程冒烟验证内核静止态门控（端口预检放行后就绪）
	sp := &fakeProbe{}
	rec2 := &eventRecorder{}
	e2 := NewEngine(newWindowsJobAPI(), sp, rec2.cb())
	e2.supStart = func(ctx context.Context, spec sup.Spec) error {
		spec.Args = []string{"/c", "ping", "-n", "30", "127.0.0.1"}
		spec.ReadyTimeout = 3 * time.Second
		sp.port = true
		return e2.sup.Start(ctx, spec)
	}
	if err := e2.Start(startOpts(mustCmdExe(t))); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = e2.Stop() }()
	waitState(t, e2, StateRunning)
	sp.running = true // 若误探测将把自己判成外部实例
	e2.RefreshExternal()
	if got := e2.Snapshot().State; got != StateRunning {
		t.Fatalf("running 状态不应被 RefreshExternal 改写，state = %s", got)
	}
}

// ---------- 配置写静默期与脱敏（纯函数） ----------

func TestConfigQuiescencePending(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if !configQuiescencePending(now.Add(-100*time.Millisecond), now) {
		t.Error("刚写完 100ms 应视为静默观察期")
	}
	if !configQuiescencePending(now.Add(-configSettleQuiet+time.Millisecond), now) {
		t.Error("差 1ms 达静默阈值仍应等待")
	}
	if configQuiescencePending(now.Add(-configSettleQuiet-time.Second), now) {
		t.Error("超过静默阈值应放行")
	}
	if configQuiescencePending(now.Add(time.Hour), now) {
		t.Error("时钟回拨等异常 mtime（未来）不应等待")
	}
}

func TestScrubSecrets(t *testing.T) {
	cases := map[string]string{
		"request https://api.dnspod.com?login_token=abc123&Domain=xx": "request https://api.dnspod.com?login_token=***&Domain=xx",
		"AccessKeyId=LTAI5tFakeKey secret=xyz":                        "AccessKeyId=*** secret=***",
		"password=hunter2 submit":                                     "password=*** submit",
		"更新成功 IP=1.2.3.4 无敏感词":                                        "更新成功 IP=1.2.3.4 无敏感词",
	}
	for in, want := range cases {
		if got := scrubSecrets(in); got != want {
			t.Errorf("scrubSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}
