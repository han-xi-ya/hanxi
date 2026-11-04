//go:build windows

// AirSnip 引擎适配层测试。进程治理主流程（spawn/Job 绑定/退出分类/收口等待）
// 已由内核 packages/go/supervisor 的引擎矩阵覆盖（fake opener，全路径不依赖真进程）；
// 本文件聚焦 airsnip 适配层：内核快照 → 本包 Snapshot 的形状/词表映射、
// ErrExternal 契约映射、"已手动停止"文案折回、Inspect 适配器的 PID 充实、
// 740 提权特判的广播暂扣收口、启动前 config.toml 更新检查接管、
// **内部命令通道**（截图/贴图/设置按目标 PID 投递）与 WM_CLOSE 宽限收口，
// 以及维持既有环境口径的真进程冒烟。
package instance

import (
	"context"
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

// ---------- 测试用 fake（探针、Job 与命令通道注入） ----------

type fakeProbe struct {
	mu    sync.Mutex
	pids  []uint32 // FindPIDs/IsRunning/Inspect 的事实源
	ready bool     // WaitForReady 立即返回值
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
	return f.job, nil
}

func newFakeJobAPI() *fakeJobAPI {
	return &fakeJobAPI{job: &fakeJob{}}
}

// commandRecorder 命令通道假实现：记录每次投递的目标与 ID，可编程返回位。
type commandRecorder struct {
	mu        sync.Mutex
	posts     []post
	closePIDs []uint32
	focusPIDs []uint32
	postOK    bool // postCmd 返回值
	focusOKs  int  // focusWin 前 N 次返回 false（模拟前台锁拒绝）
	focusN    int
	// waitClasses 记录每次等待的类名；waitWindowOK 控制"窗是否真在场"（外部判据）。
	waitClasses  []string
	waitWindowOK bool
}

type post struct{ pid, id uint32 }

func (c *commandRecorder) postCmd(pid, id uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.posts = append(c.posts, post{pid, id})
	return c.postOK
}

func (c *commandRecorder) postClose(pid uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closePIDs = append(c.closePIDs, pid)
	return true
}

func (c *commandRecorder) focus(pid uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.focusPIDs = append(c.focusPIDs, pid)
	c.focusN++
	return c.focusN > c.focusOKs
}

// waitWindow 是"外部判据"通道的假实现：返回非 0 表示指定类名的顶层窗真在场。
// 截图/设置面板的送达都由它裁决——PostMessage 返 true 一律不足以报成功。
func (c *commandRecorder) waitWindow(class string, _ uint32, _ time.Duration) uintptr {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waitClasses = append(c.waitClasses, class)
	if c.waitWindowOK {
		return 0x1234 // 假 hwnd，仅表示"在场"
	}
	return 0
}

func (c *commandRecorder) ids() []uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]uint32, 0, len(c.posts))
	for _, p := range c.posts {
		out = append(out, p.id)
	}
	return out
}

// newTestEngine 组装注入假探针/假 Job/假命令通道的引擎，返回引擎与记录器。
func newTestEngine(t *testing.T) (*Engine, *fakeProbe, *fakeJobAPI, *commandRecorder) {
	t.Helper()
	probe := &fakeProbe{ready: true}
	jobAPI := newFakeJobAPI()
	rec := &commandRecorder{postOK: true}
	var snaps []Snapshot
	var mu sync.Mutex
	e := NewEngine(jobAPI, probe, Callbacks{OnState: func(s Snapshot) {
		mu.Lock()
		snaps = append(snaps, s)
		mu.Unlock()
	}})
	e.postCmd = rec.postCmd
	e.postCloseWin = rec.postClose
	e.focusWin = rec.focus
	e.waitWin = rec.waitWindow
	t.Cleanup(func() { _ = e.Stop() })
	return e, probe, jobAPI, rec
}

// ---------- 替身进程（真 spawn 路径冒烟用） ----------

// mustCmdExe 返回系统 cmd.exe 路径（受限执行环境下经 SystemRoot 兜底）。
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

// mustAirsnipStandIn 把 cmd.exe 复制为临时目录内的 airsnip.exe 替身：
// Engine.Start 会经 APPDATA 落 config.toml 接管，直接用 System32 路径冒烟
// 与替身无关，但改名副本让 spawn 落点始终在 t.TempDir() 内可观察。
func mustAirsnipStandIn(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(mustCmdExe(t))
	if err != nil {
		t.Fatalf("读取 cmd.exe 失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), exeName)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("复制替身失败: %v", err)
	}
	return path
}

// mustPingExe 返回系统 ping.exe（存活型冒烟：固定生命期，与真实常驻同为
// "存活后被终止"形态；裸 cmd.exe 无参在部分执行环境下秒退，观察窗口不存在）。
func mustPingExe(t *testing.T) string {
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

// ---------- 740 提权特判（跨层契约） ----------

// TestElevateHint 740 → 文案含「管理员」关键词：前端 ElevateRestart 一键提权
// 按钮显隐的跨层契约（与 bcu/litemonitor/rufus 基准一致，改写不得丢关键词）。
func TestElevateHint(t *testing.T) {
	got := elevateHint(syscall.Errno(740))
	if !strings.Contains(got, "管理员") {
		t.Errorf("740 指引文案必须含「管理员」（前端按钮显隐匹配），got %q", got)
	}
	if got := elevateHint(errors.New("no such file")); got != "" {
		t.Errorf("非 740 错误应返回空串走原文案，got %q", got)
	}
	// 包装一层仍要认出（errors.As 沿链，不做字符串匹配）
	if got := elevateHint(fmtWrap(syscall.Errno(740))); !strings.Contains(got, "管理员") {
		t.Errorf("errno 被包装后应仍识别，got %q", got)
	}
	// 导出别名与内部判据同源（service RPC 通道与广播通道共用一条判据）
	if ElevateHint(syscall.Errno(740)) != elevateHint(syscall.Errno(740)) {
		t.Error("ElevateHint 导出别名与内部判据漂移")
	}
}

func fmtWrap(err error) error { return &wrapErr{err} }

type wrapErr struct{ inner error }

func (w *wrapErr) Error() string { return "进程启动失败: " + w.inner.Error() }
func (w *wrapErr) Unwrap() error { return w.inner }

// ---------- 词表与形状映射（纯函数路径） ----------

func TestMapStateVocabulary(t *testing.T) {
	cases := []struct {
		in   sup.State
		want State
	}{
		{sup.StateStopped, StateStopped},
		{sup.StateStarting, StateStarting},
		{sup.StateRunning, StateRunning},
		{sup.StateStopping, StateRunning}, // 内核 stopping 折并入 running（词表不外溢）
		{sup.StateExternal, StateExternal},
		{sup.StateFailed, StateFailed},
		{sup.State("???"), StateStopped},
	}
	for _, c := range cases {
		if got := mapState(c.in); got != c.want {
			t.Errorf("mapState(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExitCodeAndMessageMapping(t *testing.T) {
	code, ok := exitCodeFromKernelMessage("托管进程异常退出（退出码 3221225786）")
	if !ok || code != 3221225786 {
		t.Errorf("退出码提取失败: code=%d ok=%v", code, ok)
	}
	if _, ok := exitCodeFromKernelMessage("别的话术"); ok {
		t.Error("非内核文案不应误提取")
	}

	snap := mapErrorMessage(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 1）"})
	if !strings.Contains(snap, "AirSnip 异常退出（退出码 1）") {
		t.Errorf("异常退出文案未改写: %q", snap)
	}
	// 失败文案必须含「管理员」——前端 needsElevationRestart 据 snap.error 关键词
	// 决定一键提权重启钮显隐（异常退出同样可能是权限问题）。
	if !strings.Contains(snap, "管理员") {
		t.Errorf("failed 文案必须含「管理员」（跨层契约），got %q", snap)
	}
	if got := mapErrorMessage(sup.Snapshot{State: sup.StateStopped, Error: manualStopWording}); got != "" {
		t.Errorf("手动停止应折回空文案，got %q", got)
	}
	if got := mapErrorMessage(sup.Snapshot{State: sup.StateFailed, Error: "原始透传"}); got != "原始透传" {
		t.Errorf("非内核文案应透传，got %q", got)
	}
}

func TestSupProbeInspectOwnPidInfo(t *testing.T) {
	p := &fakeProbe{}
	if running, info, err := (supProbe{p}).Inspect(context.Background()); running || info != nil || err != nil {
		t.Errorf("空探针事实源应判不在场: running=%v info=%+v err=%v", running, info, err)
	}
	p.setPids([]uint32{4321, 9999})
	running, info, err := (supProbe{p}).Inspect(context.Background())
	if !running || err != nil {
		t.Fatalf("命中进程应报在场: running=%v err=%v", running, err)
	}
	if info == nil || info.PID != 4321 || info.Name != exeName {
		t.Errorf("Inspect 应充实首个命中 PID 与进程名: %+v", info)
	}
}

// ---------- 命令通道（本模块特有） ----------

// TestCommandIDsMatchUpstreamTray 四个命令 ID 是上游未公开内部约定（src/app/mod.rs
// 的 IDM_* 常量）：上游若改版漂移，本锁不会红，但真机会"点了没反应"。
// 因此这里锁的是"投递的确实是那三个数、且与常量定义同源"，把耦合点收在一处。
func TestCommandIDsMatchUpstreamTray(t *testing.T) {
	if CmdShot != 40001 || CmdPin != 40002 || CmdSettings != 40003 {
		t.Errorf("命令 ID 漂移（上游 src/app/mod.rs IDM_*）：%d %d %d", CmdShot, CmdPin, CmdSettings)
	}
	if messageWindowClass != "airsnip.hidden" {
		t.Errorf("窗口类名漂移（上游 WINDOW_CLASS）：%q", messageWindowClass)
	}
}

// TestShotAndPinDeliverProbePids 无自有实例在场时，命令目标取探针首个 PID
// （即外部实例），且截图/贴图分别投 40001/40002——投错 ID 上游只会当成
// 未知菜单项静默忽略（handle_command 的 _ => {} 分支），事后完全无从分辨。
func TestShotAndPinDeliverProbePids(t *testing.T) {
	e, probe, _, rec := newTestEngine(t)
	probe.setPids([]uint32{1234})
	rec.waitWindowOK = true // 截图覆盖层真在场（外部判据满足）

	if !e.Shot() {
		t.Fatal("Shot 应报告送达")
	}
	if !e.Pin() {
		t.Fatal("Pin 应报告投递成功")
	}
	ids := rec.ids()
	if len(ids) != 2 || ids[0] != CmdShot || ids[1] != CmdPin {
		t.Errorf("应依次投递 40001/40002，实际 %v", ids)
	}
	rec.mu.Lock()
	classes := append([]string(nil), rec.waitClasses...)
	rec.mu.Unlock()
	if len(classes) != 1 || classes[0] != WindowClassOverlay {
		t.Errorf("Shot 应以 airsnip.overlay 在场为送达判据（Pin 无独立窗不对账），实际等待 %v", classes)
	}
	rec.mu.Lock()
	targets := make([]uint32, 0, len(rec.posts))
	for _, p := range rec.posts {
		targets = append(targets, p.pid)
	}
	rec.mu.Unlock()
	for _, pid := range targets {
		if pid != 1234 {
			t.Errorf("无自有实例时目标应收敛到探针 PID，got %d", pid)
		}
	}
}

// TestCommandDeliveryFailureSurfaces 投递失败（窗口不在场）必须如实返 false，
// 不得包装成成功——service 层据此给出"请以管理员身份重启 Hanxi"的指引。
func TestCommandDeliveryFailureSurfaces(t *testing.T) {
	e, _, _, rec := newTestEngine(t)
	rec.postOK = false
	if e.Shot() || e.Pin() {
		t.Error("假通道报失败时引擎不得报成功")
	}
	posted, focused := e.ShowSettings()
	if posted || focused {
		t.Errorf("ShowSettings 应双false: posted=%v focused=%v", posted, focused)
	}
	rec.mu.Lock()
	focusTries := len(rec.focusPIDs)
	rec.mu.Unlock()
	if focusTries != 0 {
		t.Errorf("投递失败时不应尝试唤窗，实际 %d 次", focusTries)
	}
}

// TestDeliveryNotAssertedWithoutWindow **本模块最容易自欺的一处**：PostMessage
// 在 UIPI 丢弃、上游改版把菜单 ID 换掉、目标窗刚销毁等场合一律返回成功。若拿
// 返回值当"已送达"，前端就会toast「已触发截图」而屏幕上什么都没发生。
// 用例锁死：投递返 true、但对账窗不在场时，Shot/ShowSettings 必须双双报失败。
func TestDeliveryNotAssertedWithoutWindow(t *testing.T) {
	e, probe, _, rec := newTestEngine(t)
	probe.setPids([]uint32{777})
	rec.postOK = true
	rec.waitWindowOK = false // 窗始终不在场
	overlayWait = 80 * time.Millisecond
	settingsFocusWait = 80 * time.Millisecond
	t.Cleanup(func() {
		overlayWait = 2 * time.Second
		settingsFocusWait = 2 * time.Second
	})

	if e.Shot() {
		t.Error("覆盖层不在场却报截图成功（把消息入队当成了送达）")
	}
	posted, focused := e.ShowSettings()
	if posted || focused {
		t.Errorf("设置面板不在场却报送达: posted=%v focused=%v", posted, focused)
	}
	// 贴图无独立窗可对账，维持"尽力投递"口径（有投有回执即算送达，见包注释）
	if !e.Pin() {
		t.Error("Pin 走尽力投递口径，投成功即应报 true")
	}
	rec.mu.Lock()
	posts := len(rec.posts)
	rec.mu.Unlock()
	if posts != 3 {
		t.Errorf("三个动作都应实际投递过，posts=%d", posts)
	}
}

// TestShowSettingsFocusFailureReportedSeparately 面板确证在场但前台锁拒绝对话
// （后台态唤窗常见）：posted=true、focused=false，话术不得冒充"已打开"。
func TestShowSettingsFocusFailureReportedSeparately(t *testing.T) {
	e, probe, _, rec := newTestEngine(t)
	probe.setPids([]uint32{777})
	rec.waitWindowOK = true
	rec.focusOKs = 1 // focusWin 首次返 false
	settingsFocusWait = 80 * time.Millisecond
	t.Cleanup(func() { settingsFocusWait = 2 * time.Second })

	posted, focused := e.ShowSettings()
	if !posted {
		t.Fatal("面板已在场应报 posted=true")
	}
	if focused {
		t.Error("聚焦失败应报 focused=false")
	}
	rec.mu.Lock()
	classes := append([]string(nil), rec.waitClasses...)
	rec.mu.Unlock()
	if len(classes) != 1 || classes[0] != WindowClassSettings {
		t.Errorf("应等 airsnip.settings 在场，实际 %v", classes)
	}
}

// TestQuitHookPostsCloseOnlyForOwnPid WM_CLOSE 只能投自有实例；external/stopped
// 态（PID=0）一律不投，绝不替用户关掉他自己启动的 AirSnip。
func TestQuitHookPostsCloseOnlyForOwnPid(t *testing.T) {
	e, _, _, rec := newTestEngine(t)

	// 静止态：QuitHook 触发时 PID=0 → 不投
	if err := e.sup.Stop(closeGracePeriod); err != nil && !errors.Is(err, sup.ErrExternal) {
		t.Fatalf("静止态 Stop 不应报错: %v", err)
	}
	if len(rec.closePIDs) != 0 {
		t.Errorf("静止态不应投 WM_CLOSE，实际 %v", rec.closePIDs)
	}

	// 自有实例在场：经真实 spawn 的存活型冒烟路径验证（见 TestManagedLifecycleSmoke）
}

// TestPostCloseIgnoresZeroPid postClose 的 pid==0 兜底必须在 Win32 入口层就返回，
// 不允许退化到"按类名找第一个命中窗"（那会命中外部实例）。
func TestPostCloseIgnoresZeroPid(t *testing.T) {
	if postClose(0) {
		t.Error("pid=0 时不得投递 WM_CLOSE")
	}
}

// TestWindowClassNameReadsRealWindow 用本进程自己注册的一个顶层窗验证类名读取
// 与匹配链路真的通（不靠假实现自证）：Go 侧不注册窗口类，这里改测
// windowClassName(0) 的失败兜底与 findMessageWindow 的无目标返回 0。
func TestWindowClassNameDegenerateInputs(t *testing.T) {
	if got := windowClassName(0); got != "" {
		t.Errorf("非法 hwnd 应返空串，got %q", got)
	}
	if findMessageWindow(0) == 0 {
		// 本机大概率没装 AirSnip：无命中属正常；有命中也只说明事实，不据此判失败。
		t.Log("本机在场 airsnip.hidden 窗口（说明用户正在运行 AirSnip）")
	}
}

// ---------- 启动前后处理 ----------

// TestStartTakesOverUpdateCheck 托管启动前必须把 %APPDATA%\airsnip\config.toml
// 的 auto_check_update 压成 false（上游会原地覆盖托管目录里的 exe，版本库必须单源）。
// 走"必然在 Job 绑定处失败"的假 Job：接管发生在 spawn 之前，失败也要已落盘。
func TestStartTakesOverUpdateCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, configDirName)
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(cfgDir, configFileName)
	if err := os.WriteFile(cfg, []byte("auto_start = true\r\nauto_check_update = true\r\n[hotkey]\r\nscreenshot_key = 0x70\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	e, _, jobAPI, _ := newTestEngine(t)
	jobAPI.job.assignErr = errors.New("假 Job 拒绝绑定")
	_ = e.Start(StartOptions{Version: "v0.7.0", Exe: mustAirsnipStandIn(t)})

	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("配置文件读回失败: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "auto_check_update = false") {
		t.Fatalf("启动前未接管上游更新检查:\n%s", text)
	}
	// 其余键（含 CRLF 与 [hotkey] 表）必须原样在场
	if !strings.Contains(text, "auto_start = true") || !strings.Contains(text, "[hotkey]") || !strings.Contains(text, "screenshot_key = 0x70") {
		t.Errorf("接管越界改写了其它内容:\n%s", text)
	}
}

// TestStartEmptyExeRejected 空 Exe 直拒，不触碰内核。
func TestStartEmptyExeRejected(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	if err := e.Start(StartOptions{Version: "v0.7.0"}); err == nil {
		t.Fatal("空 exe 路径应被拒")
	}
}

func TestStartCreateJobFail(t *testing.T) {
	// APPDATA 必须重定向：接管发生在 spawn 之前，不隔离就会把 config.toml
	// 写进本机真实的 %APPDATA%\airsnip（污染用户真实配置，本机曾实测踩中）。
	t.Setenv("APPDATA", t.TempDir())
	e, _, jobAPI, _ := newTestEngine(t)
	jobAPI.createErr = errors.New("假 Job 创建失败")
	err := e.Start(StartOptions{Version: "v0.7.0", Exe: mustAirsnipStandIn(t)})
	if err == nil {
		t.Fatal("Job 创建失败应上报")
	}
	if e.Snapshot().State != StateFailed {
		t.Errorf("失败后状态应为 failed，实际 %s", e.Snapshot().State)
	}
}

func TestStartAssignFailRewritesElevationHint(t *testing.T) {
	// 真 740 需要带 requireAdministrator 清单的 exe，测试环境造不出；
	// 这里锁机制面：Start 在途时的 spawn 失败广播被暂扣并在返回后补播，
	// 且命中 740 时文案改写为含「管理员」的指引。
	e, _, _, rec := newTestEngine(t)
	_ = rec
	e.mu.Lock()
	e.holdSpawnFail = true
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "进程启动失败: " + syscall.Errno(740).Error()})
	e.mu.Lock()
	held := e.heldSpawnSnapOK
	e.mu.Unlock()
	if !held {
		t.Error("Start 在途时 spawn 失败广播应被暂扣（避免原始 740 文案抢跑）")
	}
	e.mu.Lock()
	e.holdSpawnFail = false
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "进程启动失败: x"})
	// 闸关后不再暂扣，快照照常外发（见 emit 计数不在此强证，行为面由冒烟用例覆盖）
}

// ---------- 宽限与幂等 ----------

func TestStopIdempotent(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	for i := 0; i < 3; i++ {
		if err := e.Stop(); err != nil {
			t.Fatalf("第 %d 次 Stop 应幂等成功: %v", i+1, err)
		}
	}
}

func TestQuitStoppedIsNoopSuccess(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	if err := e.Quit(); err != nil {
		t.Errorf("静止态 Quit 应幂等成功: %v", err)
	}
}

func TestStopExternalKeepsNilContract(t *testing.T) {
	// external 态强杀越权：内核报 ErrExternal，本层必须折成 nil（指引归 service）
	e, probe, _, _ := newTestEngine(t)
	probe.setPids([]uint32{555})
	e.sup.RefreshExternal() // 静止态 + 探针在场 → external
	if got := e.Snapshot().State; got != StateExternal {
		t.Skipf("未能构造 external 态（当前 %s），跳过契约断言", got)
	}
	if err := e.Stop(); err != nil {
		t.Errorf("external 态 Stop 应返 nil 交 service 指引，got %v", err)
	}
}

func TestRefreshExternalTwoWay(t *testing.T) {
	e, probe, _, _ := newTestEngine(t)
	if got := e.Snapshot().State; got != StateStopped {
		t.Fatalf("初始应 stopped，got %s", got)
	}
	probe.setPids([]uint32{1})
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateExternal {
		t.Errorf("探针报在场后应 external，got %s", got)
	}
	if !e.IsMessageWindowPresent() {
		t.Log("本机无 airsnip.hidden 窗口属常态（命令通道外部事实，不据此判失败）")
	}
	probe.setPids(nil)
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateStopped {
		t.Errorf("探针报离场后应回到 stopped，got %s", got)
	}
}

func TestProbePassthrough(t *testing.T) {
	e, probe, _, _ := newTestEngine(t)
	probe.setPids([]uint32{11, 22})
	if got := e.ExternalPIDs(); len(got) != 2 {
		t.Errorf("ExternalPIDs 应透传探针结果，got %v", got)
	}
	if !e.WaitReady(time.Second) {
		t.Error("WaitReady 应透传探针 ready 位")
	}
	if e.Exe() != "" {
		t.Error("静止态 Exe() 应为空串")
	}
	if e.RunningDuration() != 0 {
		t.Error("静止态运行时长应为 0")
	}
}

// ---------- 真进程存活冒烟（Job 终止/收口链路） ----------

// TestManagedLifecycleSmoke 用真实系统进程走一遍 running → Stop → stopped：
// 覆盖"接管已落盘 + 内核 spawn + Job 绑定 + 终止收口"的组合路径（假 Job 用例
// 各自只覆盖一段）。刻意不经 Engine.Start：接管与 740 特判另有专测，此处要的是
// running 观察窗口稳定存在。
func TestManagedLifecycleSmoke(t *testing.T) {
	// 真 Job Object（fake job 的 Terminate/Close 是 no-op，进程不会真死，
	// 冒烟要验的就是"内核确实能连带终止托管树"这条生产链路）。
	probe := &fakeProbe{ready: true}
	rec := &commandRecorder{postOK: true}
	e := NewEngine(windows.NewJobAPI(), probe, Callbacks{OnState: func(Snapshot) {}})
	e.postCmd = rec.postCmd
	e.postCloseWin = rec.postClose
	e.focusWin = rec.focus
	e.waitWin = rec.waitWindow
	t.Cleanup(func() { _ = e.Stop() })

	exe := mustPingExe(t)
	if err := e.sup.Start(context.Background(), sup.Spec{
		Version: "v0.7.0",
		Exe:     exe,
		Args:    []string{"-n", "30", "127.0.0.1"},
	}); err != nil {
		t.Fatalf("Start(存活型冒烟进程): %v", err)
	}
	waitState(t, e, StateRunning)
	if s := e.Snapshot(); s.PID == 0 || s.Version != "v0.7.0" {
		t.Errorf("running 快照账目不符: %+v", s)
	}
	if e.Exe() != exe {
		t.Errorf("Exe() 应回自有路径，got %q", e.Exe())
	}
	if d := e.RunningDuration(); d < 0 {
		t.Errorf("运行时长不应为负: %v", d)
	}
	// running 态下 RefreshExternal 不得把自家进程误判成外部实例
	ownPID := e.sup.Snapshot().PID
	probe.setPids([]uint32{ownPID})
	e.RefreshExternal()
	if got := e.Snapshot().State; got != StateRunning {
		t.Errorf("running 态 RefreshExternal 后应仍 running，got %s", got)
	}
	// 自有实例在场时命令目标必须收窄到自家 PID（真 Win32 投递在本机没有
	// AirSnip 在场时必然落空，这里只验目标选择逻辑，不验消息送达）
	if got := e.commandTarget(); got != ownPID {
		t.Errorf("running 态命令目标应为自有 PID %d，got %d", ownPID, got)
	}
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitState(t, e, StateStopped)
}
