package windterm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/windterm/instance"
	"hanxi/internal/modules/windterm/version"
	"hanxi/internal/platform"
)

// T2 测试加固批：根包 RPC 面。覆盖调用门拒接矩阵、版本归一、下载同步回执
// 词表（already-installed/in-progress）、后台下载租约入账与失败早退收口、
// 冷启动解析自愈、external 接管与 confirm-force 往返归因、导入自动激活。
//
// 注意：v 前缀令牌的 versioncmp 兜底排序存在字典序隐患（v2.10 < v2.9，
// 批次报告），本文件排序断言只用相邻同位数段，不把隐患钉死。

// ---------- 测试替身 ----------

type denyGateWT struct{ sentinel error }

func (g denyGateWT) Acquire(string) (func(), error) { return nil, g.sentinel }

// firstAcceptWTGate 首个 Acquire（RPC 段 Enter）放行，之后一律拒绝并记账。
// 用于把"started 之后的后台协程"变成可观察事件：若在途下载自行入账，
// 必然触发第二次 Acquire；被拒即早退，测试不触网络、不落位。
type firstAcceptWTGate struct {
	mu       sync.Mutex
	acquires int
	denied   chan struct{}
}

func (g *firstAcceptWTGate) Acquire(string) (func(), error) {
	g.mu.Lock()
	g.acquires++
	n := g.acquires
	g.mu.Unlock()
	if n == 1 {
		return func() {}, nil
	}
	select {
	case g.denied <- struct{}{}:
	default:
	}
	return nil, errors.New("模块已停用：调用门拒接")
}

type fakeProbeWT struct {
	mu       sync.Mutex
	running  bool
	info     *platform.ProcInfo
	focusAny bool
	proc     *fakeProcWT
}

func (p *fakeProbeWT) set(running bool, info *platform.ProcInfo) {
	p.mu.Lock()
	p.running, p.info = running, info
	p.mu.Unlock()
}

func (p *fakeProbeWT) setFocus(f bool) {
	p.mu.Lock()
	p.focusAny = f
	p.mu.Unlock()
}

func (p *fakeProbeWT) Inspect(context.Context, uint32) (bool, *platform.ProcInfo, error) {
	p.mu.Lock()
	running, info := p.running, p.info
	p.mu.Unlock()
	if p.proc != nil && p.proc.killed() {
		return false, nil, nil
	}
	return running, info, nil
}
func (p *fakeProbeWT) WaitForReady(time.Duration) bool { return true }
func (p *fakeProbeWT) FocusMainWindow(uint32) bool     { return false }
func (p *fakeProbeWT) FocusAnyWindow() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.focusAny
}

type fakeProcWT struct {
	mu    sync.Mutex
	info  platform.ProcInfo
	kills int
}

func (p *fakeProcWT) killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kills > 0
}

func (p *fakeProcWT) Query(uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info, nil
}

func (p *fakeProcWT) KillVerified(context.Context, platform.VerifyToken, bool) error {
	p.mu.Lock()
	p.kills++
	p.mu.Unlock()
	return nil
}

func (*fakeProcWT) IsProtected(uint32, platform.ProcInfo) bool { return false }

type fakeJobWT struct{}

func (fakeJobWT) Assign(uint32) error            { return nil }
func (fakeJobWT) Close() error                   { return nil }
func (fakeJobWT) Terminate(uint32) error         { return nil }
func (fakeJobWT) SetAllowKillOnClose(bool) error { return nil }

type fakeJobAPIWT struct{}

func (fakeJobAPIWT) Create() (platform.Job, error) { return fakeJobWT{}, nil }

// ---------- 装配辅助 ----------

func newTestWindTermService(t *testing.T) (*WindTermService, string, *fakeProbeWT, *fakeProcWT) {
	t.Helper()
	versionsDir := t.TempDir()
	proc := &fakeProcWT{}
	probe := &fakeProbeWT{proc: proc}
	svc := &WindTermService{
		manager:   version.NewManager(versionsDir),
		store:     newWindtermStore(t.TempDir()),
		engine:    instance.NewEngine(fakeJobAPIWT{}, proc, probe, instance.Callbacks{}),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	return svc, versionsDir, probe, proc
}

// seedWTInstalled 造 ListInstalled 可识别的最小假版本（windterm_<token>/WindTerm.exe）。
func seedWTInstalled(t *testing.T, versionsDir, token string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "windterm_"+token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WindTerm.exe"), []byte("MZ fake payload"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitWTRosterFree 轮询在飞名册直至销账（判据是名册实况，budget 仅挂死兜底，
// quicklook waitRosterFree 同法）。
func waitWTRosterFree(t *testing.T, svc *WindTermService, v string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		svc.downloadMu.Lock()
		_, busy := svc.downloads[v]
		svc.downloadMu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 在飞名册 %s 未销账（残账粘闸）", v, budget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---------- 版本归一（纯函数词表契约） ----------

func TestWindTermNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.6.1", "v2.6.1"},
		{"v2.6.1", "v2.6.1"},
		{"V2.6.1", "v2.6.1"},    // 大写 V 必须同落 v 规范形（旧门禁只认小写，产出 vV2.6.1）
		{" V2.6.1\t", "v2.6.1"}, // 带空白 + 大写 V
		{"  2.6.1 ", "v2.6.1"},
		{"", ""},
		{"imported-0123456789ab", "imported-0123456789ab"}, // 导入令牌原样（钉住不被补 v，本家名册口径）
		{"vimported-0123456789ab", "imported-0123456789ab"},
		{"Vimported-0123456789ab", "imported-0123456789ab"}, // 绝不产出 vvimported-
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// ---------- 下载同步回执词表（前端按状态渲染的契约） ----------

func TestWindTermDownloadSyncReceipts(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)
	seedWTInstalled(t, versionsDir, "2.16.1")

	// 已装（裸号入参归一后命中）：同步回 already-installed，不启动任何在途路。
	if state, err := svc.DownloadVersion("2.16.1"); err != nil || state != "already-installed" {
		t.Fatalf("已装版本应回 already-installed，实得 %q err=%v", state, err)
	}
	if len(svc.downloads) != 0 {
		t.Fatalf("already-installed 路径不得登记在飞名册，实得 %v", svc.downloads)
	}

	// 在飞（名册已有该归一键）：回 in-progress。
	svc.downloads["v9.9.9"] = struct{}{}
	if state, err := svc.DownloadVersion("9.9.9"); err != nil || state != "in-progress" {
		t.Fatalf("同版本连点应回 in-progress，实得 %q err=%v", state, err)
	}
	if _, ok := svc.downloads["v9.9.9"]; !ok {
		t.Fatal("in-progress 路径不得误销在飞账")
	}

	// 导入令牌免补 v 直达已装判定（normalizeVersion imported-* 原样的下游事实）。
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WindTerm.exe"), []byte("MZ fake import"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := svc.manager.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := svc.DownloadVersion(info.Version); err != nil || state != "already-installed" {
		t.Fatalf("导入件按原令牌再点下载应回 already-installed，实得 %q err=%v", state, err)
	}
}

// TestWindTermDownloadBackgroundLeaseBookkeeping 钉死 started 的后台下载
// 必须自行入账（第二次 Acquire=EnterBackground，停用 drain 数得到它），
// 且租约被拒时早退收口：名册自动销账、不落 active、目录零残骸、不触网络。
func TestWindTermDownloadBackgroundLeaseBookkeeping(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)
	gate := &firstAcceptWTGate{denied: make(chan struct{}, 4)}
	svc.holder.SetGate(gate)

	state, err := svc.DownloadVersion("9.9.9")
	if err != nil || state != "started" {
		t.Fatalf("未装且不在飞应受理 started，实得 %q err=%v", state, err)
	}
	select {
	case <-gate.denied: // 后台协程确已申请自己的租约（事件驱动，非睡眠）
	case <-time.After(5 * time.Second):
		t.Fatal("后台下载 5s 内未自行入账（未申请后台租约，drain 数不到它）")
	}
	waitWTRosterFree(t, svc, "v9.9.9", 10*time.Second)

	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("租约拒早退不得落 active，实得 %q", got)
	}
	installed, err := svc.manager.ListInstalled()
	if err != nil || len(installed) != 0 {
		t.Fatalf("早退路径不得留版本残骸，实得 %+v err=%v", installed, err)
	}
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 0 {
		t.Fatalf("versions 根目录应零残骸（含 .tmp 中转件），实得 %+v err=%v", ents, err)
	}
}

// ---------- 冷启动版本解析 ----------

func TestWindTermResolveActiveVersion(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)

	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("空树应如实报错，实得 %v", err)
	}

	seedWTInstalled(t, versionsDir, "2.16.1")
	seedWTInstalled(t, versionsDir, "2.17.0")
	v, exe, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v2.17.0" {
		t.Fatalf("未指定 active 应取更高版本（同位数段），实得 %q", v)
	}
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		t.Fatalf("返回路径须真实在场，实得 %q", exe)
	}

	// 导入件在场不得劫持托管最新版（本家族令牌形状下正确的一侧）。
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WindTerm.exe"), []byte("MZ fake import"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ierr := svc.manager.ImportLocal(src); ierr != nil {
		t.Fatal(ierr)
	}
	v2, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != "v2.17.0" {
		t.Fatalf("imported-* 兜底目录不得被排成最新，实得 %q", v2)
	}

	// active 损坏（指向不存在版本）：当场清空并持久化，回退最新已装。
	if err := svc.store.SetActive("v9.9.9"); err != nil {
		t.Fatal(err)
	}
	v3, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v3 != "v2.17.0" {
		t.Fatalf("坏 active 应回退最新已装，实得 %q", v3)
	}
	reloaded := newWindtermStore(filepath.Dir(svc.store.filePath))
	if got := reloaded.GetActive(); got != "" {
		t.Fatalf("自愈清空未落盘，重启后读到 %q", got)
	}
}

// ---------- 卸载守门（与 termora 家族同语义在 windterm 落地核对） ----------

func TestWindTermRemoveVersionGuards(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)
	seedWTInstalled(t, versionsDir, "2.16.1")
	if err := svc.store.SetActive("v2.16.1"); err != nil {
		t.Fatal(err)
	}
	// 唯一已装版本恰是使用版本：放行（不死锁），卸后 active 清空落盘。
	if err := svc.RemoveVersion("2.16.1"); err != nil {
		t.Fatalf("唯一使用中版本应放行卸载: %v", err)
	}
	if installed, err := svc.manager.ListInstalled(); err != nil || len(installed) != 0 {
		t.Fatalf("版本应已从磁盘卸载，实得 %+v err=%v", installed, err)
	}
	if got := newWindtermStore(filepath.Dir(svc.store.filePath)).GetActive(); got != "" {
		t.Fatalf("卸载使用中版本后 active 清空应持久化，实得 %q", got)
	}

	// 多版本时的使用中版本：拦截且不动账、不动目录。
	seedWTInstalled(t, versionsDir, "2.16.1")
	seedWTInstalled(t, versionsDir, "2.17.0")
	if err := svc.store.SetActive("v2.16.1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveVersion("2.16.1"); err == nil || !strings.Contains(err.Error(), "请先选择其他版本") {
		t.Fatalf("多版本时使用中版本应被拒，实得 %v", err)
	}
	if got := svc.store.GetActive(); got != "v2.16.1" {
		t.Fatalf("被拒后 active 不得变动，实得 %q", got)
	}
	if installed, _ := svc.manager.ListInstalled(); len(installed) != 2 {
		t.Fatalf("被拒后目录数不得减少，实得 %d", len(installed))
	}
}

// ---------- external 接管与 confirm-force 往返归因 ----------

func TestWindTermQuitExternalConfirmRoundTrip(t *testing.T) {
	svc, _, probe, proc := newTestWindTermService(t)
	info := &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\WindTerm.exe`, StartedAt: time.Now()}
	proc.info = *info
	probe.set(true, info)
	svc.engine.RefreshExternal()
	if !svc.engine.ExternalRunning() {
		t.Fatal("前置：应已接管为 external")
	}

	// 首入（未授权）：只回 confirm-required，绝不触达终止；风险文案与
	// 消息同源（确认框与回执共用一个事实源）。
	out, err := svc.Quit(false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "confirm-required" || out.Method != "confirm-required" || out.External != true {
		t.Fatalf("首入应回 confirm-required，实得 %+v", out)
	}
	if out.Stopped || out.Forced || out.CloseRequested {
		t.Fatalf("未授权路径必须零动作，实得 %+v", out)
	}
	if out.Risk == "" || out.Risk != out.Message {
		t.Fatalf("confirm-required 的风险文案须与消息同源非空，实得 Risk=%q Message=%q", out.Risk, out.Message)
	}
	if proc.kills != 0 {
		t.Fatalf("未授权路径不得触达终止，实得 kills=%d", proc.kills)
	}
	if !svc.engine.ExternalRunning() {
		t.Fatal("confirm-required 后外部实例应保持在场")
	}

	// 授权重入：优雅段（伪造 PID 无可投窗口）不生效 → 同意后强杀收口。
	out2, err := svc.Quit(true)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Method != "forced" || !out2.Stopped || !out2.Forced || out2.CloseRequested {
		t.Fatalf("confirm-force 收口归因应 forced/Stopped/!CloseRequested，实得 %+v", out2)
	}
	if proc.kills != 1 {
		t.Fatalf("恰一次终止调用，实得 %d", proc.kills)
	}
	if got := svc.engine.Snapshot().State; got != instance.StateStopped {
		t.Fatalf("强杀收口后应回 stopped（不留幽灵 external），实得 %s", got)
	}
}

func TestWindTermQuitExternalUnidentifiedRefusesEvenWithConfirm(t *testing.T) {
	svc, _, probe, proc := newTestWindTermService(t)
	probe.set(true, nil) // 在场但身份不明
	out, err := svc.Quit(true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "probe-missing-pid" || out.Stopped || out.Forced {
		t.Fatalf("身份不明必须拒执行（confirm=true 也不放行），实得 %+v", out)
	}
	if proc.kills != 0 {
		t.Fatal("拒执行路径不得触达终止（不猜身份）")
	}
}

func TestWindTermQuitNotManaged(t *testing.T) {
	svc, _, _, proc := newTestWindTermService(t)
	out, err := svc.Quit(false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "not-managed" || out.Stopped || out.External {
		t.Fatalf("静止会话应归因 not-managed，实得 %+v", out)
	}
	if proc.kills != 0 {
		t.Fatal("not-managed 不得触达终止")
	}
}

func TestWindTermOpenWindowExternalVerdict(t *testing.T) {
	svc, _, probe, _ := newTestWindTermService(t)
	probe.set(true, &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\WindTerm.exe`, StartedAt: time.Now()})

	probe.setFocus(false)
	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "external-unreachable" || !out.External {
		t.Fatalf("无可聚焦窗口须如实回 unreachable（绝不代起第二实例），实得 %+v", out)
	}

	probe.setFocus(true)
	out2, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out2.Action != "external-focused" || !out2.External {
		t.Fatalf("聚焦成功应回 external-focused，实得 %+v", out2)
	}
}

// ---------- 导入自动激活 ----------

func TestWindTermImportLocalAutoActivatesOnlyWhenUnset(t *testing.T) {
	svc, _, _, _ := newTestWindTermService(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WindTerm.exe"), []byte("MZ fake payload A"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := svc.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != info.Version {
		t.Fatalf("无 active 时导入应自动激活，实得 %q 期望 %q", got, info.Version)
	}

	src2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(src2, "WindTerm.exe"), []byte("MZ fake payload B"), 0o644); err != nil {
		t.Fatal(err)
	}
	info2, err := svc.ImportLocal(src2)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != info.Version {
		t.Fatalf("已设 active 时导入不得抢账，实得 %q（后来者 %q）", got, info2.Version)
	}
}

// ---------- 调用门拒接矩阵 ----------

func TestWindTermGateRefusesRPCSurface(t *testing.T) {
	svc, _, _, _ := newTestWindTermService(t)
	sentinel := errors.New("模块已停用：调用门拒接")
	svc.holder.SetGate(denyGateWT{sentinel: sentinel})

	if _, err := svc.ListReleases(); !errors.Is(err, sentinel) {
		t.Errorf("ListReleases 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.ListInstalledVersions(); !errors.Is(err, sentinel) {
		t.Errorf("ListInstalledVersions 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.DownloadVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("DownloadVersion 门拒应上抛原错，实得 %v", err)
	}
	if len(svc.downloads) != 0 {
		t.Errorf("门拒路径不得登记在飞名册，实得 %v", svc.downloads)
	}
	if _, err := svc.ImportLocal(`C:\nonexistent`); !errors.Is(err, sentinel) {
		t.Errorf("ImportLocal 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.RemoveVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("RemoveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.SetActiveVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("SetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetActiveVersion(); !errors.Is(err, sentinel) {
		t.Errorf("GetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetStatus(); !errors.Is(err, sentinel) {
		t.Errorf("GetStatus 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.OpenWindow(); !errors.Is(err, sentinel) {
		t.Errorf("OpenWindow 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.Quit(false); !errors.Is(err, sentinel) {
		t.Errorf("Quit 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetFollowOnExit(); !errors.Is(err, sentinel) {
		t.Errorf("GetFollowOnExit 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.SetFollowOnExit(true); !errors.Is(err, sentinel) {
		t.Errorf("SetFollowOnExit 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.Shutdown(); !errors.Is(err, sentinel) {
		t.Errorf("Shutdown(RPC) 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.RepositoryURL(); !errors.Is(err, sentinel) {
		t.Errorf("RepositoryURL 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.OpenRepository(); !errors.Is(err, sentinel) {
		t.Errorf("OpenRepository 门拒应上抛原错（plat 未接线，越过门即 panic），实得 %v", err)
	}
}
