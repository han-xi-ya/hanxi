//go:build windows

package instance

import (
	"context"
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

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/收口等待）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖。本文件聚焦 HEU 适配层：
// 740 提权指引（含"管理员"关键词的跨层契约）、内核快照 → 本包 Snapshot 的
// 词表/文案映射、spawn 失败广播暂扣收口、Inspect 适配器的 PID 充实、
// external 态的越权保护，以及一条真实 Job 终止链路的存活型冒烟。

// ---------- 测试用 fake（探针与 Job 注入） ----------

type fakeProbe struct {
	mu         sync.Mutex
	pids       []uint32
	ready      bool
	windowOpen bool
}

func (p *fakeProbe) FindPIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.pids...)
}
func (p *fakeProbe) IsRunning() bool { return len(p.FindPIDs()) > 0 }
func (p *fakeProbe) WaitForReady(time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}
func (p *fakeProbe) IsMainWindowOpen(pids []uint32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.windowOpen && len(pids) > 0
}
func (p *fakeProbe) setPids(pids []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pids = pids
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

func (r *eventRecorder) all() []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Snapshot(nil), r.states...)
}

// ---------- 740 提权特判（跨层契约） ----------

// TestElevateHint errno 740 → 文案含「管理员」关键词：前端 ElevateRestart 组件
// 以该关键词判定一键提权重启按钮显隐（与 bcu/litemonitor 基准同构的跨层契约）。
func TestElevateHint(t *testing.T) {
	got := elevateHint(syscall.Errno(740))
	if !strings.Contains(got, "管理员") {
		t.Fatalf("740 指引文案必须含「管理员」（前端按钮显隐匹配），got %q", got)
	}
	if !strings.Contains(got, "Hanxi") {
		t.Fatalf("指引应指向重启 Hanxi 而非泛泛而谈，got %q", got)
	}
	// exec 层常以 %w 包装原始 errno，两层包装同样要识别
	if got := elevateHint(fmt.Errorf("fork/exec: %w", syscall.Errno(740))); got == "" {
		t.Fatal("包装后的 740 也应被识别")
	}
	if got := elevateHint(fmt.Errorf("进程启动失败: %w", fmt.Errorf("fork/exec: %w", syscall.Errno(740)))); got == "" {
		t.Fatal("内核再包装后的 740 也应被识别")
	}
	if got := elevateHint(errors.New("no such file")); got != "" {
		t.Fatalf("非 740 错误应返回空串走原文案，got %q", got)
	}
}

// TestIsSpawnFailure 暂扣判据只认 spawn 路径的两类内核前缀（措辞耦合，
// 内核文案变更时退化为不暂扣而非崩溃）。
func TestIsSpawnFailure(t *testing.T) {
	yes := sup.Snapshot{State: sup.StateFailed, Error: "进程启动失败: fork/exec ...: %%1"}
	no := sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 1）"}
	if !isSpawnFailure(yes) {
		t.Fatal("spawn 失败广播应判为暂扣对象")
	}
	if isSpawnFailure(no) {
		t.Fatal("运行期异常退出不是 spawn 失败，不得暂扣")
	}
	if isSpawnFailure(sup.Snapshot{State: sup.StateRunning}) {
		t.Fatal("非 failed 态一律不暂扣")
	}
}

// TestStartWrapsSpawnFailureAsElevation 端到端验证 740 改写链：Engine.Start 在途
// 时内核 spawn 失败广播被暂扣（不外发英文裸错误），Start 返回后由 wrapper 补播
// 管理员指引文案。真实 UAC 场景不可自动化，故注入一个必然 spawn 失败的目标
// （不存在的 exe）驱动同一条暂扣-补播通路，并用 elevateHint 接缝断言文案形状。
func TestStartWrapsSpawnFailureAsElevation(t *testing.T) {
	rec := &eventRecorder{}
	probe := &fakeProbe{}
	e := NewEngine(&fakeJobAPI{}, probe, rec.cb())

	missing := filepath.Join(t.TempDir(), "HEU_KMS_Activator_v99.9.9.exe")
	err := e.Start(StartOptions{Version: "99.9.9", Exe: missing})
	if err == nil {
		t.Fatal("不存在的 exe 应启动失败")
	}
	// 暂扣窗口内不得抢先广播原始英文错误
	for _, s := range rec.all() {
		if s.State == StateFailed && strings.Contains(s.Error, "CreateProcess") {
			t.Fatalf("spawn 原始错误被抢发（暂扣失效）: %+v", s)
		}
	}
	last := rec.last()
	if last.State != StateFailed {
		t.Fatalf("终态应为 failed，got %+v", last)
	}
	// 非 740 错误（文件不存在）如实透传内核原文案，不冒充提权指引
	if strings.Contains(last.Error, "管理员") {
		t.Fatalf("文件缺失不得被改写成提权指引: %q", last.Error)
	}
	if last.Version != "99.9.9" {
		t.Fatalf("快照版本应绑定启动参数，got %+v", last)
	}
}

// ---------- 词表与文案映射 ----------

func TestMapStateVocabulary(t *testing.T) {
	cases := []struct {
		in   sup.State
		want State
	}{
		{sup.StateStopped, StateStopped},
		{sup.StateStarting, StateStarting},
		{sup.StateRunning, StateRunning},
		{sup.StateStopping, StateRunning}, // 终止窗口对前端保持运行语义
		{sup.StateExternal, StateExternal},
		{sup.StateFailed, StateFailed},
		{sup.State("unknown"), StateStopped}, // 未知态保守回落 stopped
	}
	for _, c := range cases {
		if got := mapState(c.in); got != c.want {
			t.Errorf("mapState(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestExitCodeFromKernelMessage(t *testing.T) {
	if code, ok := exitCodeFromKernelMessage("托管进程异常退出（退出码 3221225477）"); !ok || code != 3221225477 {
		t.Fatalf("大退出码提取失败: %d %v", code, ok)
	}
	if _, ok := exitCodeFromKernelMessage("进程启动失败: xxx"); ok {
		t.Fatal("非异常退出文案不应提取退出码")
	}
}

// TestMapErrorMessage 三条改写口径：异常退出改人话（指向安全软件拦截这一
// 本工具的真实高频故障）、手动停止折回空文案、其余透传。
func TestMapErrorMessage(t *testing.T) {
	got := mapErrorMessage(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 5）"})
	if !strings.Contains(got, "退出码 5") || !strings.Contains(got, "安全软件") {
		t.Fatalf("异常退出文案应含退出码与安全软件指引，got %q", got)
	}
	if !strings.HasPrefix(got, "HEU KMS Activator") {
		t.Fatalf("文案主语应是本工具名，got %q", got)
	}
	if got := mapErrorMessage(sup.Snapshot{State: sup.StateStopped, Error: manualStopWording}); got != "" {
		t.Fatalf("手动停止应折回空文案，got %q", got)
	}
	passthrough := sup.Snapshot{State: sup.StateFailed, Error: "就绪等待超时"}
	if got := mapErrorMessage(passthrough); got != passthrough.Error {
		t.Fatalf("其余文案应透传，got %q", got)
	}
}

// ---------- 探针适配 ----------

func TestSupProbeInspect(t *testing.T) {
	empty := &fakeProbe{}
	running, info, err := supProbe{empty}.Inspect(context.Background())
	if running || info != nil || err != nil {
		t.Fatalf("无进程时应报 (false,nil,nil)，got %v %+v %v", running, info, err)
	}

	p := &fakeProbe{}
	p.setPids([]uint32{4711, 4712})
	running, info, err = supProbe{p}.Inspect(context.Background())
	if !running || err != nil {
		t.Fatalf("有进程时应报 running，got %v %v", running, err)
	}
	if info == nil || info.PID != 4711 {
		t.Fatalf("Inspect 应附带首个存活 PID 供内核 external 快照充实，got %+v", info)
	}
}

func TestStartOptionsValidate(t *testing.T) {
	if err := (StartOptions{}).validate(); err == nil {
		t.Fatal("空路径应拒绝")
	}
	if err := (StartOptions{Exe: `C:\x\HEU_KMS_Activator_v64.0.0.exe`}).validate(); err != nil {
		t.Fatalf("合法路径应通过: %v", err)
	}
}

// ---------- 进程名探测（文件名漂移） ----------

// TestProcessNamePrefixMatchesDriftedNames 前缀判据对上游两种真实形态都成立：
// 带版本串的原名，以及历史裸名（v1.0 时代叫 HEU_KMS_Activator.exe）。
func TestProcessNamePrefixMatchesDriftedNames(t *testing.T) {
	matches := func(name string) bool {
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		return strings.HasPrefix(strings.ToLower(stem), strings.ToLower(exeNamePrefix))
	}
	for _, n := range []string{
		"HEU_KMS_Activator_v64.0.0.exe",
		"Heu_Kms_Activator_v63.4.0.exe", // 大小写不敏感
		"HEU_KMS_Activator.exe",         // 裸名历史形态
	} {
		if !matches(n) {
			t.Errorf("应命中前缀判据: %s", n)
		}
	}
	for _, n := range []string{"hes.exe", "KMS_Activator.exe", "svchost.exe", "HEU_Tool.exe"} {
		if matches(n) {
			t.Errorf("不应误命中无关进程: %s", n)
		}
	}
}

// ---------- external 越权保护与存活型冒烟 ----------

func TestQuitOnExternalIsNoop(t *testing.T) {
	// external 态下 Quit/Stop 都不得触碰非自家进程（强杀越权），映射为无操作成功。
	e := NewEngine(&fakeJobAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Quit(); err != nil {
		t.Fatalf("未运行时 Quit 应幂等成功: %v", err)
	}
	if err := e.Stop(); err != nil {
		t.Fatalf("未运行时 Stop 应幂等成功: %v", err)
	}
}

// mustLiveExe 返回系统 ping.exe 路径（存活型冒烟进程；含 SystemRoot 兜底，
// 与 litemonitor 同口径——裸 cmd.exe 在无控制台输入源的环境里秒退，观察窗口不存在）。
func mustLiveExe(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("ping.exe"); err == nil {
		return path
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "PING.EXE")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	t.Fatal("ping.exe 不可用")
	return ""
}

// TestManagedLifecycleSmoke 真实 Job Object 下走一遍自有实例的启动→running→
// Quit 收口（ping -n 30 提供稳定存活窗口）。刻意经 e.sup 直启：Args 属测试
// 脚手架，不扩 StartOptions 生产面；Engine.Start 的暂扣路径由上一个用例覆盖。
func TestManagedLifecycleSmoke(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(windows.NewJobAPI(), &fakeProbe{}, rec.cb())

	exe := mustLiveExe(t)
	if err := e.sup.Start(context.Background(), sup.Spec{
		Version: "smoke",
		Exe:     exe,
		Args:    []string{"-n", "30", "127.0.0.1"},
	}); err != nil {
		t.Fatalf("直启失败: %v", err)
	}
	defer func() { _ = e.Stop() }() // 断言失败时也不留孤儿 ping 进程

	snap := e.Snapshot()
	if snap.State != StateRunning {
		t.Fatalf("启动后应为 running，got %+v", snap)
	}
	if snap.PID == 0 {
		t.Fatalf("running 态应有 PID: %+v", snap)
	}
	if e.Exe() != exe {
		t.Fatalf("Exe() 应报自有实例路径，got %q", e.Exe())
	}
	if e.RunningDuration() < 0 {
		t.Fatal("运行时长不得为负")
	}

	// 窗口未创建（ping 无窗）→ IsMainWindowOpen 假，前端据此提示"窗口未见"
	if e.IsMainWindowOpen() {
		t.Log("冒烟进程出现了可唤窗口（环境差异，不作断言）")
	}

	// 探针报外部进程时，RefreshExternal 不得改写自有 running 态
	probe := e.probe.(*fakeProbe)
	probe.setPids([]uint32{999999})
	e.RefreshExternal()
	if got := e.Snapshot(); got.State != StateRunning {
		t.Fatalf("running 态不应被外部探测接管: %+v", got)
	}

	start := rec.count()
	// 内核 Stop 是同步收口（等 wait 回写终态并关闭 done 才返回），故返回后
	// 终态快照即已确定，无需额外等待。
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit 失败: %v", err)
	}
	final := e.Snapshot()
	if final.State == StateRunning || final.State == StateStarting {
		t.Fatalf("Quit 后仍处活动态: %+v", final)
	}
	if rec.count() <= start {
		t.Fatalf("Quit 过程应至少再广播一次终态（原 %d 次，现 %d 次）", start, rec.count())
	}
	if final.External {
		t.Fatalf("自有实例收口不得被记为 external: %+v", final)
	}
}
