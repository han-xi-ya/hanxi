package piik

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	piikinstance "hanxi/internal/modules/piik/instance"
)

// ---------- 测试用假件（假 A 面：版本引擎 / 实例引擎 / 端口试绑 / 浏览器） ----------
//
// 全 seam 装配：零真实下载、零真实进程、零真实端口占用、零真实浏览器唤起。
// 实例假件按 A 线实面语义造景——Start 把 Options.Port 记进 Snapshot.Port、
// RefreshExternal 只对静止态翻 external，本层读端口的代码路径与真实引擎一致。

type fakeManager struct {
	mu          sync.Mutex
	installed   []VersionInfo
	resolveErr  map[string]error
	removeCalls []string
	drifted     bool
	driftNote   string
	importInfo  VersionInfo
	importErr   error
}

func (f *fakeManager) ListRemote() ([]Release, error) { return nil, nil }

func (f *fakeManager) ListInstalled() ([]VersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installed, nil
}

func (f *fakeManager) DownloadContext(context.Context, string, string, func(DownloadProgress)) error {
	return errors.New("fakeManager 不承载真实下载（测试必须注入 downloadDriver）")
}

func (f *fakeManager) ImportLocal(string) (VersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.importErr != nil {
		return VersionInfo{}, f.importErr
	}
	return f.importInfo, nil
}

// Remove 卸载即版本目录消失——假件如实复刻这条语义（从 installed 里剔除），
// 否则"末版卸空后给下载指引"的路径会被假件虚假地保留成"还在装"。
func (f *fakeManager) Remove(targetVersion string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCalls = append(f.removeCalls, targetVersion)
	kept := f.installed[:0]
	for _, v := range f.installed {
		if v.Version != targetVersion {
			kept = append(kept, v)
		}
	}
	f.installed = kept
	return nil
}

func (f *fakeManager) ResolveExe(targetVersion string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.resolveErr[targetVersion]; err != nil {
		return "", err
	}
	for _, v := range f.installed {
		if v.Version == targetVersion {
			return v.ExePath, nil
		}
	}
	return "", errors.New("版本未安装: " + targetVersion)
}

func (f *fakeManager) VerifyLedger(string) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drifted, f.driftNote
}

func (f *fakeManager) CheckUpdate(context.Context) (string, string, bool, error) {
	return "", "", false, nil
}

// fakeEngine 假 A 线实例引擎：记账式，不起进程。
//   - Start 成功 → 快照 running 并把 Options.Port 记进 Port（A 线同款"分配即入账"）；
//   - Quit/Stop → 快照 stopped（端口账按 A 线语义留在原位，本层不得依赖它归零）；
//   - RefreshExternal 只在静止态按 externalOn 翻 external（running 时探测到的
//     正是自己，A 线纪律如实复刻）；
//   - waitReady=false 模拟"就绪等不到"（进程起了但端口没 LISTEN）。
type fakeEngine struct {
	mu         sync.Mutex
	starts     []piikinstance.Options
	quits      int
	stops      int
	refreshes  int
	waitReady  bool
	snapshot   piikinstance.Snapshot
	startErr   error
	externalOn bool
}

func (e *fakeEngine) Start(opts piikinstance.Options) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startErr != nil {
		return e.startErr
	}
	e.starts = append(e.starts, opts)
	e.snapshot = piikinstance.Snapshot{
		Version: opts.Version,
		State:   piikinstance.StateRunning,
		PID:     4242,
		Port:    opts.Port,
	}
	return nil
}

func (e *fakeEngine) Quit() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.quits++
	e.snapshot.State = piikinstance.StateStopped
	e.snapshot.PID = 0
	return nil
}

func (e *fakeEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stops++
	e.snapshot.State = piikinstance.StateStopped
	e.snapshot.PID = 0
	return nil
}

func (e *fakeEngine) Snapshot() piikinstance.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshot
}

func (e *fakeEngine) RefreshExternal() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshes++
	if !e.externalOn {
		return
	}
	switch e.snapshot.State {
	case piikinstance.StateRunning, piikinstance.StateStarting:
		return // 自有实例在世时探测到的正是自己，不翻 external
	default:
		e.snapshot = piikinstance.Snapshot{State: piikinstance.StateExternal, External: true, PID: 9999}
	}
}

func (e *fakeEngine) WaitReady(time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.waitReady
}

func (e *fakeEngine) startCalls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.starts)
}

func (e *fakeEngine) lastStart() piikinstance.Options {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.starts) == 0 {
		return piikinstance.Options{}
	}
	return e.starts[len(e.starts)-1]
}

func (e *fakeEngine) setRunning(version string, port int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshot = piikinstance.Snapshot{
		Version: version,
		State:   piikinstance.StateRunning,
		PID:     4242,
		Port:    port,
	}
}

func (e *fakeEngine) setState(state piikinstance.State) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshot.State = state
}

// newTestPiikService 装配单测最小 service（dbx harness 同法：结构体字面量注入；
// 调用门未挂 = Enter/EnterBackground 直通，ops 账本未注入 = 事务降级 no-op）。
// 端口试绑/拨测/浏览器三个 seam 全换假件，绝不动真端口、绝不开真浏览器。
func newTestPiikService(t *testing.T) (*PiikService, *fakeManager, *fakeEngine, *[]string) {
	t.Helper()
	dir := t.TempDir()
	var opened []string
	eng := &fakeEngine{waitReady: true, snapshot: piikinstance.Snapshot{State: piikinstance.StateStopped}}
	svc := &PiikService{
		manager:    &fakeManager{},
		store:      newPiikStore(dir),
		holder:     extapi.NewLeaseHolder(moduleID),
		downloads:  map[string]struct{}{},
		dataDir:    filepath.Join(dir, "piik"),
		configPath: filepath.Join(dir, "piik", configFileName),
		logDir:     filepath.Join(dir, "piik", logDirName),
		engine:     eng,
	}
	svc.bindProbe = func(int) bool { return true }
	svc.connectProbe = func(int) bool { return true }
	svc.browserOpen = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	return svc, svc.manager.(*fakeManager), eng, &opened
}

// ---------- 端口分配器 ----------

func TestAllocatePortSkipsBusyPorts(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	busy := map[int]bool{8787: true, 8788: true}
	svc.bindProbe = func(port int) bool { return !busy[port] }

	port, err := svc.allocatePort()
	if err != nil {
		t.Fatalf("allocatePort: %v", err)
	}
	if port != 8789 {
		t.Fatalf("8787/8788 被占应上移到 8789, got %d", port)
	}
}

func TestAllocatePortBoundedScanHonestError(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	probed := 0
	svc.bindProbe = func(int) bool { probed++; return false }

	_, err := svc.allocatePort()
	if err == nil {
		t.Fatal("全部候选被占必须报错")
	}
	if probed != portScanLimit {
		t.Fatalf("扫描必须有界（≤%d 次试绑），实得 %d", portScanLimit, probed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "8787") || !strings.Contains(msg, "8796") || !strings.Contains(msg, "端口查杀") {
		t.Fatalf("错误文案须点名端口区间与处置通道: %s", msg)
	}
}

// TestDefaultPortSingleSource 端口常量单点钉死：service 的 defaultPort 必须就是
// A 线 instance.DefaultPort（两处各写 8787 是漂移的开始）。
func TestDefaultPortSingleSource(t *testing.T) {
	if defaultPort != piikinstance.DefaultPort {
		t.Fatalf("defaultPort=%d 与 A 线 DefaultPort=%d 不同源", defaultPort, piikinstance.DefaultPort)
	}
}

// portBindable 真实试绑自证（不依赖 A 线与 seam，用本机临时监听占位）：
// 真被占住的端口必须判不可用，刚放开的端口必须判可绑。
func TestPortBindableRealBind(t *testing.T) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{})
	if err != nil {
		t.Skipf("本机无法开测试监听: %v", err)
	}
	defer ln.Close()
	occupied := ln.Addr().(*net.TCPAddr).Port
	if portBindable(occupied) {
		t.Fatalf("端口 %d 已被本测试占住，试绑应判不可用", occupied)
	}

	probe, err := net.ListenTCP("tcp", &net.TCPAddr{})
	if err != nil {
		t.Skipf("本机无法取空闲端口: %v", err)
	}
	free := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	if !portBindable(free) {
		t.Fatalf("刚放开的端口 %d 应判可绑", free)
	}
}

// ---------- 启动编排：分配端口与托管数据落点随 Options 下传 ----------

func TestStartPassesPortAndManagedDataPathsDown(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	svc.bindProbe = func(port int) bool { return port != 8787 } // 8787 被他人占

	out, err := svc.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if out.Action != "started" || out.Port != 8788 {
		t.Fatalf("启动回执异常: %+v", out)
	}
	opts := eng.lastStart()
	if opts.Port != 8788 {
		t.Fatalf("分配端口未下传引擎: %+v", opts)
	}
	if opts.ConfigPath != svc.configPath || opts.LogDir != svc.logDir {
		t.Fatalf("托管数据落点未下传引擎: cfg=%q log=%q", opts.ConfigPath, opts.LogDir)
	}
	if opts.Exe == "" || opts.Version != "v1.6.5" {
		t.Fatalf("版本解析未下传: %+v", opts)
	}
	if !opts.Detached {
		t.Fatal("followOnExit 默认 false → Detached 必须为 true（家族统一语义）")
	}
	if got := svc.store.GetActive(); got != "v1.6.5" {
		t.Fatalf("首次冷启动应回写 activeVersion, got %q", got)
	}
	// 数据目录与日志目录必须已落位（不赌上游对不存在目录的行为）
	if fi, err := os.Stat(svc.dataDir); err != nil || !fi.IsDir() {
		t.Fatalf("数据目录未创建: %v", err)
	}
	if fi, err := os.Stat(svc.logDir); err != nil || !fi.IsDir() {
		t.Fatalf("日志目录未创建: %v", err)
	}
	// 端口账单一来源：引擎快照记的账就是本层对外说的账
	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.ListenPort != 8788 || st.ConsoleURL != piikinstance.URLForPort(8788) {
		t.Fatalf("端口/界面地址应同源: listenPort=%d consoleUrl=%q", st.ListenPort, st.ConsoleURL)
	}
}

func TestStartFollowOnExitMakesInstanceAttached(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	if err := svc.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	if _, err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if eng.lastStart().Detached {
		t.Fatal("开关联动时 Detached 必须为 false（Job 罩死，孙进程同灭）")
	}
}

func TestStartWaitReadyFailurePointsAtPortNotWebView2(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	eng.waitReady = false

	_, err := svc.Start()
	if err == nil {
		t.Fatal("就绪等不到必须报错")
	}
	msg := err.Error()
	if strings.Contains(msg, "WebView2") {
		t.Fatalf("piik 无 WebView2 依赖，照抄窗口托管模板措辞即错: %s", msg)
	}
	if !strings.Contains(msg, "8787") || !strings.Contains(msg, "端口查杀") {
		t.Fatalf("失败文案须点名端口与处置通道: %s", msg)
	}
}

func TestStartExternalNeverTakesOver(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.externalOn = true

	out, err := svc.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if out.Action != "external" || !out.External {
		t.Fatalf("external 场应如实回执: %+v", out)
	}
	if eng.startCalls() != 0 {
		t.Fatal("external 场绝不二次拉起")
	}
}

func TestStartRunningIdempotent(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8790)

	out, err := svc.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if out.Action != "already-running" || out.Port != 8790 || !strings.Contains(out.URL, "8790") {
		t.Fatalf("幂等直返异常: %+v", out)
	}
	if eng.startCalls() != 0 {
		t.Fatal("已运行不得二次拉起")
	}
}

func TestStartStartingCriticalSectionNoSecondSpawn(t *testing.T) {
	svc, _, eng, opened := newTestPiikService(t)
	eng.setState(piikinstance.StateStarting)
	for name, call := range map[string]func() (ControlOutcome, error){"Start": svc.Start, "OpenWindow": svc.OpenWindow} {
		out, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.Action != "starting" {
			t.Fatalf("%s 启动临界区应如实回执: %+v", name, out)
		}
	}
	if eng.startCalls() != 0 || len(*opened) != 0 {
		t.Fatalf("临界区不得动作: starts=%d opened=%v", eng.startCalls(), *opened)
	}
}

func TestStartNoVersionGuidance(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	if _, err := svc.Start(); err == nil {
		t.Fatal("零版本启动必须报错")
	} else if !strings.Contains(err.Error(), "下载或导入") {
		t.Fatalf("末版卸空后的指引缺失: %v", err)
	}
	if eng.startCalls() != 0 {
		t.Fatal("无版本可解析时不得调用引擎")
	}
}

func TestEngineBusyErrorPropagates(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	eng.startErr = errors.New("instance: 上一个 piik 进程尚未收口，请稍后重试")

	if _, err := svc.Start(); err == nil || !strings.Contains(err.Error(), "尚未收口") {
		t.Fatalf("引擎 errBusy 必须如实透传不吞: %v", err)
	}
}

// ---------- 打开界面 OpenWindow（= 系统浏览器，无窗服务的"唤窗"等价物） ----------

func TestOpenWindowRunningReopensConsoleURL(t *testing.T) {
	svc, _, eng, opened := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8787)

	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if out.Action != "opened" || out.URL != piikinstance.URLForPort(8787) {
		t.Fatalf("打开界面回执异常: %+v", out)
	}
	if len(*opened) != 1 || (*opened)[0] != piikinstance.URLForPort(8787) {
		t.Fatalf("应恰好唤起一次浏览器: %v", *opened)
	}
	if eng.startCalls() != 0 {
		t.Fatal("已运行时不得二次拉起")
	}
}

// TestOpenWindowNeverColdStarts 两动词分工的负裁决回归位（C 线前端同口径：
// 「启动」走 Start，「打开界面」走 OpenWindow）：起服务等于在局域网敞开一个
// 分享端口，"界面打不开"绝不构成替机主开后台服务的理由。
func TestOpenWindowNeverColdStarts(t *testing.T) {
	svc, fm, eng, opened := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}

	_, err := svc.OpenWindow()
	if err == nil {
		t.Fatal("stopped 态打开界面必须如实拒绝")
	}
	if !strings.Contains(err.Error(), "请先点「启动」") {
		t.Fatalf("拒绝文案缺「启动」指引: %v", err)
	}
	if eng.startCalls() != 0 || len(*opened) != 0 {
		t.Fatalf("拒绝路径不得起服务也不得开浏览器: starts=%d opened=%v", eng.startCalls(), *opened)
	}
	// 同景下 Start 才是该走的路（两个动词互补，缺一即页面无法到达）
	if out, err := svc.Start(); err != nil || out.Action != "started" {
		t.Fatalf("Start 应能冷启动: %+v %v", out, err)
	}
}

// TestOpenWindowBrowserFailureIsHonest 浏览器唤起失败：如实报错并保留服务，
// 给出可手动访问的地址（前端 noBrowser 兜底位之外，回执也得说清实况）。
func TestOpenWindowBrowserFailureIsHonest(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8787)
	svc.browserOpen = func(string) error { return errors.New("无可用默认浏览器") }

	_, err := svc.OpenWindow()
	if err == nil || !strings.Contains(err.Error(), "浏览器") {
		t.Fatalf("浏览器失败必须如实上抛: %v", err)
	}
	if eng.quits != 0 || eng.stops != 0 {
		t.Fatalf("开不成页面不该动服务: quits=%d stops=%d", eng.quits, eng.stops)
	}
}

func TestOpenWindowExternalLocatesPortAndLabelsNonManaged(t *testing.T) {
	svc, _, eng, opened := newTestPiikService(t)
	eng.externalOn = true
	svc.connectProbe = func(port int) bool { return port == defaultPort }

	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatalf("OpenWindow external: %v", err)
	}
	if out.Action != "external-opened" || !out.External {
		t.Fatalf("external 回执异常: %+v", out)
	}
	if len(*opened) != 1 || (*opened)[0] != piikinstance.URLForPort(defaultPort) {
		t.Fatalf("未指向外部实例实占端口: %v", *opened)
	}
	if !strings.Contains(out.Message, "非 Hanxi 托管") {
		t.Fatalf("external 界面须如实标注非托管: %q", out.Message)
	}
	if eng.startCalls() != 0 {
		t.Fatal("external 场绝不二次拉起，也不越权停它")
	}
}

func TestOpenWindowExternalUnreachablePortErrors(t *testing.T) {
	svc, _, eng, opened := newTestPiikService(t)
	eng.externalOn = true
	svc.connectProbe = func(int) bool { return false }

	if _, err := svc.OpenWindow(); err == nil {
		t.Fatal("外部进程在场但候选端口全不可达必须如实报错")
	}
	if len(*opened) != 0 {
		t.Fatal("定位失败不得开浏览器")
	}
}

// ---------- GetStatus 复合投影 ----------

func TestGetStatusPassesThroughMachineFieldsWithoutPasswordValue(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	fm.drifted, fm.driftNote = true, "主 exe mtime 新于落位账"
	eng.setRunning("v1.6.5", 8789)
	// 机读字段按 A 线快照原样入账（口令明文无处可存：类型里没有那个字段）
	eng.mu.Lock()
	eng.snapshot.LocalAccessOpen = true
	eng.snapshot.PasswordSet = true
	eng.snapshot.LanInvitation = "http://192.168.1.20:8789/"
	eng.snapshot.PublicInvitation = "https://demo-cf.trycloudflare.com/"
	eng.snapshot.NoBrowser = true
	eng.mu.Unlock()

	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.PasswordSet || !st.LocalAccessOpen || !st.NoBrowser {
		t.Fatalf("机读布尔未原样展平: %+v", st.Snapshot)
	}
	if st.LanInvitation == "" || st.PublicInvitation == "" {
		t.Fatalf("邀请链接未原样转呈: %+v", st.Snapshot)
	}
	if st.ConsoleURL != piikinstance.URLForPort(8789) {
		t.Fatalf("界面地址投影异常: %q", st.ConsoleURL)
	}
	if st.DataDir == "" || st.ConfigPath == "" || st.LogDir == "" {
		t.Fatalf("托管数据目录三账缺失: %+v", st)
	}
	if !st.Drifted || st.DriftNote == "" {
		t.Fatalf("漂移投影失败: %+v", st)
	}

	// "口令值绝不上前端"的结构位证明：序列化真实 A 线快照类型，只允许出现
	// passwordSet 布尔；任何 "password" 字符串键（值形态）出现即违规——本断言
	// 同时是未来的护栏：谁往 A 线快照里加口令字段，这里当场红。
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, `"password":`) {
		t.Fatalf("GetStatus 载荷出现口令值键: %s", body)
	}
	if !strings.Contains(body, `"passwordSet":true`) {
		t.Fatalf("GetStatus 载荷缺口令有无布尔: %s", body)
	}
	if !strings.Contains(body, `"consoleUrl"`) || !strings.Contains(body, `"lanInvitation"`) {
		t.Fatalf("前端消费键名未在载荷中: %s", body)
	}
}

func TestGetStatusStoppedGivesNoConsoleURL(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	// 引擎按 A 线语义把最近一代端口留在账上，但服务已停：界面链接必须收口
	eng.setRunning("v1.6.5", 8790)
	eng.setState(piikinstance.StateStopped)

	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.ConsoleURL != "" {
		t.Fatalf("未运行不得给可点界面链接: %q", st.ConsoleURL)
	}
}

func TestGetStatusExternalGivesNoConsoleURL(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.externalOn = true

	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.State != piikinstance.StateExternal || st.ConsoleURL != "" {
		t.Fatalf("external 态界面链接不归本层背书: %+v", st)
	}
	// external 的 listenPort 给上游默认口：前端横幅"通常占着 8787"与本处同源，
	// 绝不留 0 让前端各写一份猜测。
	if st.ListenPort != defaultPort {
		t.Fatalf("external 态 listenPort 应为默认口, got %d", st.ListenPort)
	}
	if eng.refreshes == 0 {
		t.Fatal("GetStatus 前应做一次外部校正")
	}
}

// ---------- 下载事件时序（termora 2ac9b3b 同型病灶回归护栏） ----------

type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

// 锁死"done 先落账后广播"：前端收到 done 即复刷 GetActiveVersion，广播位上
// active 必须已有值（断言取广播位快照，不事后补读，免与被测代码赛跑）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(p DownloadProgress) {
		seen <- downloadReceipt{stage: p.Stage, version: p.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(DownloadProgress)) error {
		emit(DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	if state, err := svc.DownloadVersion("1.6.5"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v1.6.5" {
		t.Fatalf("done 回执版本应为归一化的 v1.6.5，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v1.6.5" {
		t.Fatalf("done 广播位上 active 应已落账，实得 %q", rr.activeAtBroadcast)
	}

	// 已钉住的版本不得被后续下载覆盖——日更风暴下这条尤其要紧。
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(DownloadProgress)) error {
		emit(DownloadProgress{Version: targetVersion, Stage: "done", Done: 1, Total: 1})
		return nil
	}
	if state, err := svc.DownloadVersion("1.7.0"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.activeAtBroadcast != "v1.6.5" {
		t.Fatalf("钉住的版本不应被新下载改写，广播位实得 %q", rr.activeAtBroadcast)
	}
}

func waitDoneReceipt(t *testing.T, seen <-chan downloadReceipt) downloadReceipt {
	t.Helper()
	for {
		select {
		case r := <-seen:
			if r.stage == "done" {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatal("等待下载 done 事件超时")
			return downloadReceipt{}
		}
	}
}

func TestDownloadVersionAlreadyInstalledAndDedupe(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	if state, err := svc.DownloadVersion("v1.6.5"); err != nil || state != "already-installed" {
		t.Fatalf("已装版本应短路: state=%q err=%v", state, err)
	}
}

// ---------- 版本解析与卸载清账 ----------

func TestResolveActiveVersionPrefersAndSelfHeals(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{
		{Version: "v1.4.0", ExePath: `X:\piik_1.4.0\piik-app.exe`},
		{Version: "v1.10.0", ExePath: `X:\piik_1.10.0\piik-app.exe`},
		{Version: "v1.9.0", ExePath: `X:\piik_1.9.0\piik-app.exe`},
	}

	if err := svc.store.SetActive("v1.4.0"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	v, exe, err := svc.resolveActiveVersion()
	if err != nil || v != "v1.4.0" || !strings.HasSuffix(exe, "1.4.0\\piik-app.exe") {
		t.Fatalf("active 优先失败: %v %v %v", v, exe, err)
	}

	// 设定版本被卸载/损坏 → 清空自愈回退数值最新（字典序会把 1.10.0 判给 1.9.0 之后）
	if err := svc.store.SetActive("v9.9.9"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	v, _, err = svc.resolveActiveVersion()
	if err != nil || v != "v1.10.0" {
		t.Fatalf("应回退数值最新 v1.10.0: %v %v", v, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("失效设定应被清空自愈, got %q", got)
	}
}

func TestRemoveVersionAllowsLastVersionAndClearsLedger(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	if err := svc.store.SetActive("v1.6.5"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	if err := svc.RemoveVersion("1.6.5"); err != nil {
		t.Fatalf("末版卸载应放行（第一天口径）: %v", err)
	}
	if len(fm.removeCalls) != 1 || fm.removeCalls[0] != "v1.6.5" {
		t.Fatalf("应归一化 v 前缀后委托 manager: %v", fm.removeCalls)
	}
	if svc.store.GetActive() != "" {
		t.Fatal("卸掉设定版本后 activeVersion 应清空")
	}
	// 卸空后模块不死锁：给的是"先下载或导入"指引
	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "下载或导入") {
		t.Fatalf("零版本应给指引, got %v", err)
	}
}

func TestRemoveVersionRunningRejected(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8787)
	if err := svc.RemoveVersion("v1.6.5"); err == nil {
		t.Fatal("正在运行的版本必须拒卸")
	}
	if len(fm.removeCalls) != 0 {
		t.Fatal("拒卸路径不得触盘")
	}
}

func TestSetActiveVersionRejectsUninstalled(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	if _, err := svc.SetActiveVersion("v9.9.9"); err == nil {
		t.Fatal("未安装版本不得设成使用版本")
	}
}

func TestImportLocalSetsActiveWhenEmpty(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.importInfo = VersionInfo{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}
	if _, err := svc.ImportLocal(`X:\downloads\piik`); err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if got := svc.store.GetActive(); got != "v1.6.5" {
		t.Fatalf("N24 契约：首个到手版本应=使用版本, got %q", got)
	}
}

func TestImportLocalRejectedWhileInstanceInWorld(t *testing.T) {
	svc, fm, eng, _ := newTestPiikService(t)
	fm.importErr = errors.New("不该被调用")
	// ImportLocal 不做外部校正（读的是引擎既有快照），故直接把快照摆成外部在场
	eng.setState(piikinstance.StateExternal)
	_, err := svc.ImportLocal(`X:\downloads\piik`)
	if err == nil || !strings.Contains(err.Error(), "请先退出再导入") {
		t.Fatalf("运行/外部实例在场时应由本层闸口拒导入（不得落到 manager）: %v", err)
	}
}

// ---------- 退出编排与如实预告 ----------

func TestQuitExternalInstanceNotKilled(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.externalOn = true
	eng.RefreshExternal()

	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("external 场应零越权: %+v", out)
	}
	if eng.quits != 0 {
		t.Fatal("external 场绝不向引擎下退出指令")
	}
	if !strings.Contains(out.Message, "外部") {
		t.Fatalf("external 指引缺失: %q", out.Message)
	}
}

func TestQuitOwnedReportsReleasedPort(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8788)

	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if !out.Stopped || out.External || out.Port != 8788 {
		t.Fatalf("干净退出回执异常: %+v", out)
	}
	if eng.quits != 1 {
		t.Fatalf("优雅停通道应走引擎 Quit, got %d", eng.quits)
	}
	if !strings.Contains(out.Message, "8788") {
		t.Fatalf("退出回执应点名刚释放的端口: %q", out.Message)
	}
}

func TestQuitWithoutPortLedgerIsHonest(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	// 从未启动过（端口账 0）：不硬凑"端口 0 已释放"的假账
	eng.snapshot = piikinstance.Snapshot{State: piikinstance.StateStopped}
	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if out.Port != 0 || !strings.Contains(out.Message, "未曾分配端口") {
		t.Fatalf("无端口账时的诚实回执异常: %+v", out)
	}
}

func TestQuitAdvisorySinglePointHonestAboutAudienceDrop(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8787)
	msg, err := svc.QuitAdvisory()
	if err != nil {
		t.Fatalf("QuitAdvisory: %v", err)
	}
	for _, needle := range []string{"观众", "断播", "链接"} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("服务在跑时预告必须点名断播代价，缺 %q: %s", needle, msg)
		}
	}

	// external 场：先把自有实例收口（A 线 RefreshExternal 只对静止态生效，
	// running 时探测到的正是自己），再让探针见同名外部进程在场
	eng.setState(piikinstance.StateStopped)
	eng.externalOn = true
	eng.RefreshExternal()
	if eng.Snapshot().State != piikinstance.StateExternal {
		t.Fatalf("假件未翻 external（静止态纪律走样）: %+v", eng.Snapshot())
	}
	msg, err = svc.QuitAdvisory()
	if err != nil {
		t.Fatalf("QuitAdvisory external: %v", err)
	}
	if !strings.Contains(msg, "不会终止") {
		t.Fatalf("external 场预告须说明不越权: %s", msg)
	}
}

// ---------- metaHints 六条披露账 ----------

func TestMetaHintsSixDisclosures(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	hints, err := svc.MetaHints()
	if err != nil {
		t.Fatalf("MetaHints: %v", err)
	}
	if len(hints) != 6 {
		t.Fatalf("metaHints 应恰好六条（风险登记表④逐条对位），实得 %d:\n%s", len(hints), strings.Join(hints, "\n"))
	}
	joined := strings.Join(hints, "\n")
	for _, needle := range []string{
		"钉版本",        // 1 日更风暴手动追
		"0.0.0.0",    // 2 局域网暴露如实
		"Cloudflare", // 3 公网隧道
		"不代管",        // 4 --link 由 Web UI 侧发起
		"JobObject",  // 5 capture/隧道子进程受笼
		svc.dataDir,  // 6 数据留托管目录（路径点名）
		"不删数据",       // 6 卸载明示
	} {
		if !strings.Contains(joined, needle) {
			t.Errorf("metaHints 缺披露位 %q:\n%s", needle, joined)
		}
	}
	if strings.Contains(joined, "WebView2") {
		t.Error("piik 无 WebView2 依赖，披露账里不该出现")
	}
}

func TestMetaHintsHonestWhenNothingRunning(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	hints, err := svc.MetaHints()
	if err != nil {
		t.Fatalf("MetaHints: %v", err)
	}
	if !strings.Contains(hints[0], "未指定") {
		t.Fatalf("未钉版本时须如实说明自动取最新已装: %s", hints[0])
	}
	if !strings.Contains(hints[1], "默认端口 8787") {
		t.Fatalf("未运行时端口披露须给默认口而非编造当前口: %s", hints[1])
	}

	if _, err := svc.SetActiveVersion("v1.6.5"); err == nil {
		t.Skip("无已装版本，钉版分支另测")
	}
	fm := svc.manager.(*fakeManager)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	if _, err := svc.SetActiveVersion("v1.6.5"); err != nil {
		t.Fatalf("SetActiveVersion: %v", err)
	}
	hints, _ = svc.MetaHints()
	if !strings.Contains(hints[0], "已钉 v1.6.5") {
		t.Fatalf("钉版后披露账应点名版本: %s", hints[0])
	}
}

// ---------- 数据目录导航 ----------

func TestOpenDataDirHonestWhenAbsent(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	if err := svc.OpenDataDir(); err == nil {
		t.Fatal("目录未创建时应如实报错（未托管启动过）")
	} else if !strings.Contains(err.Error(), "尚未创建") {
		t.Fatalf("报错文案应点名未创建事实: %v", err)
	}
	if err := svc.ensureDataDir(); err != nil {
		t.Fatalf("ensureDataDir: %v", err)
	}
	// 目录就位后走平台层 explorer（本机可能不可用），错误须如实透传不静默。
	if err := svc.OpenDataDir(); err != nil {
		t.Logf("OpenDataDir 平台层回执（本机 explorer 不可用属预期）: %v", err)
	}
}

// ---------- 联动开关与收口纪律 ----------

func TestFollowOnExitRoundTrip(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	if v, err := svc.GetFollowOnExit(); err != nil || v {
		t.Fatalf("默认应 false: %v %v", v, err)
	}
	if err := svc.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	if v, err := svc.GetFollowOnExit(); err != nil || !v {
		t.Fatalf("回转失败: %v %v", v, err)
	}
}

// TestActivateOnlyWatchesNeverIdleKills 服务型骨架负裁决回归位：后台巡检只有
// "外部实例感知"一路（本模块无空闲自动退出代码路径——服务在跑就有会话价值，
// 闲置 ≠ 可杀）；收口后 watcher 归零且可重复激活。
func TestActivateOnlyWatchesNeverIdleKills(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	eng.setRunning("v1.6.5", 8787)
	svc.activate()
	svc.activate() // 幂等：不得起第二路巡检
	if !svc.watching {
		t.Fatal("activate 应挂起巡检")
	}
	svc.shutdown()
	if svc.watching {
		t.Fatal("shutdown 应收口巡检")
	}
	if eng.quits != 0 || eng.stops != 0 {
		t.Fatalf("运行中的托管服务不该被巡检杀掉（且 followOnExit 关）: quits=%d stops=%d", eng.quits, eng.stops)
	}
}

// TestShutdownRespectsFollowOnExitSwitch 联动关闭（默认）时 shutdown 绝不动
// 引擎（服务跨 Hanxi 生命周期继续开播正是该开关的意义）；开启后走 Stop 强杀
// 通道（不耗宽限，Job 连带回收 cloudflared/piik-capture）。
func TestShutdownRespectsFollowOnExitSwitch(t *testing.T) {
	svc, _, eng, _ := newTestPiikService(t)
	svc.activate()
	svc.shutdown()
	if eng.stops != 0 {
		t.Fatalf("联动关闭时不得停实例, got %d", eng.stops)
	}

	s2, _, eng2, _ := newTestPiikService(t)
	if err := s2.store.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2.activate()
	s2.shutdown()
	if eng2.stops != 1 {
		t.Fatalf("联动开启应走 Stop 通道, got %d", eng2.stops)
	}
}

func TestRepositoryURLIsUpstream(t *testing.T) {
	svc, _, _, _ := newTestPiikService(t)
	url, err := svc.RepositoryURL()
	if err != nil {
		t.Fatalf("RepositoryURL: %v", err)
	}
	if !strings.HasPrefix(url, "https://github.com/") || !strings.Contains(url, "Piik") {
		t.Fatalf("上游地址异常: %s", url)
	}
}

func TestOpenRepositoryUsesSameSource(t *testing.T) {
	svc, _, _, opened := newTestPiikService(t)
	if err := svc.OpenRepository(); err != nil {
		t.Fatalf("OpenRepository: %v", err)
	}
	want, _ := svc.RepositoryURL()
	if len(*opened) != 1 || (*opened)[0] != want {
		t.Fatalf("打开仓库与展示地址必须同源: opened=%v want=%q", *opened, want)
	}
}
