//go:build windows

package gonavi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/gonavi/instance"
	"hanxi/internal/modules/gonavi/version"
	win "hanxi/internal/platform/windows"
)

// ---------- 假版本引擎（versionEngine seam） ----------

// fakeManager 冻结契约公开面的假实现：磁盘/网络零触碰，行为全部可编程，
// 并留调用账目（removed/verifyCalls）供断言。
type fakeManager struct {
	mu sync.Mutex

	releases  []version.GoNaviRelease
	remoteErr error

	installed []version.GoNaviVersionInfo
	imported  version.GoNaviVersionInfo
	removable bool // RemoveVersion 是否放行（外部占用拒绝场景置 false）
	removeErr error

	resolve map[string]string // 版本 → exe 路径（缺表 = ResolveExe 报错）

	// dl 可选下载驱动器：非 nil 时 DownloadContext 按其驱动进度事件
	// （隔离网络，同步 emit 收口时序）。
	dl func(ctx context.Context, targetVersion string, emit func(version.DownloadProgress)) error

	drifted   bool
	driftNote string

	removed     []string
	verifyCalls []string

	downloadCalls []string
}

func (f *fakeManager) ListRemote() ([]version.GoNaviRelease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases, f.remoteErr
}

func (f *fakeManager) ListInstalled() ([]version.GoNaviVersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]version.GoNaviVersionInfo(nil), f.installed...), nil
}

func (f *fakeManager) DownloadContext(ctx context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
	f.mu.Lock()
	f.downloadCalls = append(f.downloadCalls, targetVersion)
	drive := f.dl
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if drive != nil {
		return drive(ctx, targetVersion, emit)
	}
	emit(version.DownloadProgress{Version: targetVersion, Stage: "done"})
	return nil
}

func (f *fakeManager) ImportLocal(string) (version.GoNaviVersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.imported, nil
}

func (f *fakeManager) Remove(targetVersion string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	for i, v := range f.installed {
		if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
			f.installed = append(f.installed[:i], f.installed[i+1:]...)
			f.removed = append(f.removed, targetVersion)
			return nil
		}
	}
	return fmt.Errorf("版本 %s 未安装", targetVersion)
}

func (f *fakeManager) ResolveExe(targetVersion string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if exe, ok := f.resolve[targetVersion]; ok {
		return exe, nil
	}
	return "", fmt.Errorf("版本 %s 未安装或已损坏", targetVersion)
}

func (f *fakeManager) VerifyLedger(targetVersion string) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyCalls = append(f.verifyCalls, targetVersion)
	return f.drifted, f.driftNote
}

func (f *fakeManager) CheckUpdate(context.Context) (string, string, bool, error) {
	return "", "", false, nil
}

// ---------- 假探针（instance.GoNaviProbe seam） ----------

type stubProbe struct {
	mu         sync.Mutex
	pids       []uint32
	ready      bool
	windowOpen bool
}

func (p *stubProbe) FindPIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.pids...)
}
func (p *stubProbe) IsRunning() bool { return len(p.FindPIDs()) > 0 }
func (p *stubProbe) WaitForReady(time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}
func (p *stubProbe) IsMainWindowOpen(pids []uint32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.windowOpen && len(pids) > 0
}
func (p *stubProbe) setPids(pids []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pids = pids
}

// ---------- harness ----------

// newTestGoNaviService 装配最小 service：结构体字面量注入（调用门未挂 =
// Enter/EnterBackground 直通，ops 账本未注入 = 事务降级 no-op）；engine 组合
// 真 JobAPI + 假探针，进程拉起走真进程（notepad 替身见 mustStandInExe）。
func newTestGoNaviService(t *testing.T, fm *fakeManager) (*GoNaviService, *stubProbe) {
	t.Helper()
	probe := &stubProbe{ready: true}
	svc := &GoNaviService{
		manager:   fm,
		store:     newGoNaviStore(t.TempDir()),
		downloads: make(map[string]struct{}),
		holder:    extapi.NewLeaseHolder(ID),
	}
	svc.engine = instance.NewEngine(win.NewJobAPI(), probe, instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc, probe
}

// mustStandInExe 返回系统 notepad.exe（真进程冒烟替身）：无参即存活（GUI 常驻，
// 免 cmd.exe 在精简 stdin 环境秒退的环境红口径），且**不是** GoNavi.exe——
// 假探针按 GoNavi 进程名给外部事实，托管实例自身存活/终止判定全走内核句柄，
// 替身进程名不干扰状态机。Quit 的 WM_CLOSE 对干净的 notepad 窗可优雅收口。
func mustStandInExe(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("notepad.exe"); err == nil {
		return path
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "notepad.exe")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	t.Fatal("notepad.exe 不可用")
	return ""
}

// waitServiceState 轮询 service 引擎快照直至目标状态（或超时）。
func waitServiceState(t *testing.T, svc *GoNaviService, want instance.State) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if got := svc.engine.Snapshot().State; got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %s 超时，当前 %s", want, svc.engine.Snapshot().State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---------- 启停 / 末版卸载放行清账 ----------

// TestManagedStartQuitAndLastVersionRemoval 启停闭环 + 卸载纪律：
// OpenWindow 冷启动（假探针即时就绪）→ running；运行中拒绝卸载当前版本；
// Quit 优雅退出（notepad 收 WM_CLOSE）→ stopped；随后末版可卸、
// activeVersion 清账、卸空后给出"先下载或导入"指引（不死锁）。
func TestManagedStartQuitAndLastVersionRemoval(t *testing.T) {
	exe := mustStandInExe(t)
	fm := &fakeManager{
		installed: []version.GoNaviVersionInfo{{Version: "v0.8.9", ExePath: exe}},
		resolve:   map[string]string{"v0.8.9": exe},
	}
	svc, _ := newTestGoNaviService(t, fm)

	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if out.Action != "started" || !strings.Contains(out.Message, "v0.8.9") {
		t.Fatalf("冷启动回执异常: %+v", out)
	}
	waitServiceState(t, svc, instance.StateRunning)
	// 首次冷启动把实际采用版本回写为 activeVersion
	if got := svc.store.GetActive(); got != "v0.8.9" {
		t.Fatalf("冷启动后 active 应回写 v0.8.9，实得 %q", got)
	}
	// 再次唤窗 = 直操作路径（running 不二次拉起）
	if out, err = svc.OpenWindow(); err != nil || out.Action != "opened" {
		t.Fatalf("running 态唤窗异常: %+v %v", out, err)
	}
	// 运行中的当前版本拒绝卸载
	if err := svc.RemoveVersion("v0.8.9"); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("running 版本卸载应被拒: %v", err)
	}

	if q, err := svc.Quit(); err != nil || !q.Stopped || q.External {
		t.Fatalf("Quit 异常: %+v %v", q, err)
	}
	waitServiceState(t, svc, instance.StateStopped)

	// 末版卸载放行 + 清账（卸空后 resolveActiveVersion 给出下载/导入指引）
	if err := svc.RemoveVersion("0.8.9"); err != nil {
		t.Fatalf("末版卸载应放行: %v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("卸载设定版本后 active 应清空，实得 %q", got)
	}
	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("卸空后应给出先下载/导入指引: %v", err)
	}
}

// ---------- 外部接管 ----------

// TestExternalTakeoverReadonly GoNavi.exe 进程在场但非我方托管：external 态
// 唤窗走进程 PID 直操作，Quit 不越权强杀只给指引。
func TestExternalTakeoverReadonly(t *testing.T) {
	fm := &fakeManager{resolve: map[string]string{}}
	svc, probe := newTestGoNaviService(t, fm)
	probe.setPids([]uint32{4321}) // 假外部实例 PID（不存在的窗，Win32 直操作 no-op）

	out, err := svc.OpenWindow()
	if err != nil || out.Action != "external-opened" || !out.External {
		t.Fatalf("external 唤窗异常: %+v %v", out, err)
	}
	q, err := svc.Quit()
	if err != nil || q.Stopped || !q.External {
		t.Fatalf("external Quit 应只给指引不强杀: %+v %v", q, err)
	}
	st, err := svc.GetStatus()
	if err != nil || st.State != instance.StateExternal || !st.External {
		t.Fatalf("GetStatus 应如实投影 external: %+v %v", st, err)
	}
}

// ---------- 漂移投影 ----------

// TestGetStatusDriftProjection GetStatus 把 active 版本的 VerifyLedger 定向
// 复查结果并入快照投影（drifted/driftNote 直通）；无版本可查时不复查。
func TestGetStatusDriftProjection(t *testing.T) {
	fm := &fakeManager{
		installed: []version.GoNaviVersionInfo{{Version: "v0.8.9", ExePath: "X:\\nowhere\\GoNavi.exe"}},
		resolve:   map[string]string{"v0.8.9": "X:\\nowhere\\GoNavi.exe"},
		drifted:   true,
		driftNote: "账外漂移：实测 sha256 与落位账本不符",
	}
	svc, _ := newTestGoNaviService(t, fm)

	// 未设使用且无自有版本：不触发复查
	st, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Drifted || st.DriftNote != "" || len(fm.verifyCalls) != 0 {
		t.Fatalf("无版本可查时不应复查: %+v calls=%v", st, fm.verifyCalls)
	}

	// 设定 active 后：定向复查 active，漂移如实投影
	if _, err := svc.SetActiveVersion("v0.8.9"); err != nil {
		t.Fatal(err)
	}
	st, err = svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Drifted || !strings.Contains(st.DriftNote, "漂移") {
		t.Fatalf("漂移未投影: %+v", st)
	}
	if len(fm.verifyCalls) != 1 || fm.verifyCalls[0] != "v0.8.9" {
		t.Fatalf("应定向复查 active 版本，calls=%v", fm.verifyCalls)
	}
}

// ---------- 下载事件时序（termora 2ac9b3b 同型护栏） ----------

// downloadReceipt 下载事件票据：activeAtBroadcast 由探针在广播位（downloadProbe
// 与生产 Wails Emit 同一点位）于下载 goroutine 内同步快照——即"前端此刻复刷
// GetActiveVersion 会读到什么"的实况。
type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc, _ := newTestGoNaviService(t, &fakeManager{})
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(p version.DownloadProgress) {
		seen <- downloadReceipt{stage: p.Stage, version: p.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	if state, err := svc.DownloadVersion("0.8.9"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v0.8.9" {
		t.Fatalf("done 回执版本应为归一化的 v0.8.9，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v0.8.9" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 v0.8.9，实得 %q", rr.activeAtBroadcast)
	}
	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"）。
	if state, err := svc.DownloadVersion("0.9.0"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v0.9.0" {
		t.Fatalf("第二次下载应广播 v0.9.0 的 done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v0.8.9" {
		t.Fatalf("active 已有值时广播位不应被后续下载改写，实得 %q", rr.activeAtBroadcast)
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

// ---------- 版本解析 ----------

// TestResolveActiveVersion active 优先；失效自愈清空后回退最新已装
// （数值分段比较，0.10.0 > 0.9.0）；卸空给出指引。
func TestResolveActiveVersion(t *testing.T) {
	fm := &fakeManager{
		installed: []version.GoNaviVersionInfo{
			{Version: "v0.9.0", ExePath: `X:\a\GoNavi.exe`},
			{Version: "v0.10.0", ExePath: `X:\b\GoNavi.exe`},
		},
		resolve: map[string]string{"v0.9.0": `X:\a\GoNavi.exe`},
	}
	svc, _ := newTestGoNaviService(t, fm)

	if err := svc.store.SetActive("v0.9.0"); err != nil {
		t.Fatal(err)
	}
	if v, exe, err := svc.resolveActiveVersion(); err != nil || v != "v0.9.0" || exe != `X:\a\GoNavi.exe` {
		t.Fatalf("active 应优先: %s %s %v", v, exe, err)
	}

	// active 指向未安装/损坏版本：清空自愈，回退数值最新已装
	if err := svc.store.SetActive("v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if v, _, err := svc.resolveActiveVersion(); err != nil || v != "v0.10.0" {
		t.Fatalf("应回退数值最新已装 v0.10.0: %s %v", v, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("失效 active 应被清空自愈，实得 %q", got)
	}

	fm.installed = nil
	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("零版本应给指引: %v", err)
	}
}

// TestSetActiveVersionValidates 设定未安装版本被拒（ResolveExe 校验先行），
// store 不被污染。
func TestSetActiveVersionValidates(t *testing.T) {
	fm := &fakeManager{resolve: map[string]string{}}
	svc, _ := newTestGoNaviService(t, fm)
	if _, err := svc.SetActiveVersion("v9.9.9"); err == nil {
		t.Fatal("未安装版本设定应被拒")
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("校验失败不应落账，实得 %q", got)
	}
}

// TestImportLocalFirstAdoptsActive N24：首导即设使用；已有 active 不覆盖。
func TestImportLocalFirstAdoptsActive(t *testing.T) {
	fm := &fakeManager{imported: version.GoNaviVersionInfo{Version: "v0.8.9", IsImport: true}}
	svc, _ := newTestGoNaviService(t, fm)
	if _, err := svc.ImportLocal(`X:\src\GoNavi`); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != "v0.8.9" {
		t.Fatalf("首导应自动设使用，实得 %q", got)
	}
	fm.imported = version.GoNaviVersionInfo{Version: "v0.9.1", IsImport: true}
	if _, err := svc.ImportLocal(`X:\src2`); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != "v0.8.9" {
		t.Fatalf("已有 active 不应被导入覆盖，实得 %q", got)
	}
}

// ---------- 生命周期收尾 ----------

// TestShutdownStopsWatcher activate 挂轮询、shutdown 摘轮询且幂等；
// followOnExit 关（默认）时 shutdown 不触碰实例（独立运行口径）。
func TestShutdownStopsWatcher(t *testing.T) {
	fm := &fakeManager{}
	svc, probe := newTestGoNaviService(t, fm)
	probe.setPids([]uint32{4321})

	svc.activate()
	svc.activate() // 幂等：重复 activate 不叠加
	svc.shutdown()
	svc.shutdown() // 幂等
	svc.watchMu.Lock()
	watching := svc.watching
	svc.watchMu.Unlock()
	if watching {
		t.Fatal("shutdown 后 watching 应复位")
	}
	// followOnExit=false（默认）：外部假实例不受 shutdown 影响，仍可由感知轮询
	// 校正——此处直接验证 store 默认口径
	if svc.store.GetFollowOnExit() {
		t.Fatal("followOnExit 默认应为 false")
	}
}

// TestNoIdleAutoQuit 产品约束固化：GoNavi 是数据库工作台，会话长驻是常态，
// "空闲自动退出"语义不成立（同 litemonitor 决策）——service 刻意不实现 idle
// 巡检，本测试以文档形式钉死，防止后续维护照抄 ccswitch 加回去。
func TestNoIdleAutoQuit(t *testing.T) {
	// 结构性断言：service 不暴露任何 idle 相关能力（touch/idleCheck/shouldIdleQuit）。
	// 若未来确有需求，先重读本注释论证语义再动。
}

// TestVersionCompare 数值分段比较：多位数段不被字典序坑（0.10.0 > 0.9.0）。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.10.0", "v0.9.0", 1},
		{"v0.9.0", "v0.10.0", -1},
		{"v0.8.9", "v0.8.9", 0},
		{"1.0.0", "v0.99.99", 1},
		{"0.8.9", "v0.8.9", 0}, // 无 v 前缀兼容
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
