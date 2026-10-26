package rammap

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
	"hanxi/internal/modules/rammap/instance"
	"hanxi/internal/modules/rammap/version"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// T2 测试加固批：根包（RPC 面）此前仅覆盖 settle 时序，本文件补齐
// 调用门拒接矩阵、冷启动版本解析（含 active 损坏自愈落盘）、提权预告
// 投影、external 接管/退出归因与唤窗裁决等真实外部行为。
//
// 已知生产缺陷（报告批次，勿在本文件钉死）：resolveActiveVersion 的
// versioncmp 兜底排序对日期令牌与 imported-*/v* 导入令牌一律走字典序，
// 'i'/'v' > '2' 会让导入目录在 active 未设时被排成"最新"。故本文件的
// 排序断言只使用日期令牌族（字典序=时间序是该族的既定设计）。

// ---------- 测试替身 ----------

type denyGateRammap struct{ sentinel error }

func (g denyGateRammap) Acquire(string) (func(), error) { return nil, g.sentinel }

type fakeProbeRammap struct {
	mu        sync.Mutex
	running   bool
	info      *platform.ProcInfo
	focusAny  bool
	proc      *fakeProcRammap
	focusCall int
}

func (p *fakeProbeRammap) set(running bool, info *platform.ProcInfo) {
	p.mu.Lock()
	p.running, p.info = running, info
	p.mu.Unlock()
}

func (p *fakeProbeRammap) setFocus(f bool) {
	p.mu.Lock()
	p.focusAny = f
	p.mu.Unlock()
}

func (p *fakeProbeRammap) Inspect(context.Context, uint32) (bool, *platform.ProcInfo, error) {
	p.mu.Lock()
	running, info := p.running, p.info
	p.mu.Unlock()
	if p.proc != nil && p.proc.killed() {
		return false, nil, nil // 强杀收口联动：探针看不见已终止实例
	}
	return running, info, nil
}
func (p *fakeProbeRammap) WaitForReady(time.Duration) bool { return true }
func (p *fakeProbeRammap) FocusMainWindow(uint32) bool     { return false }
func (p *fakeProbeRammap) FocusAnyWindow() bool {
	p.mu.Lock()
	p.focusCall++
	f := p.focusAny
	p.mu.Unlock()
	return f
}

type fakeProcRammap struct {
	mu    sync.Mutex
	info  platform.ProcInfo
	kills int
}

func (p *fakeProcRammap) killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kills > 0
}

func (p *fakeProcRammap) Query(uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info, nil
}

func (p *fakeProcRammap) KillVerified(context.Context, platform.VerifyToken, bool) error {
	p.mu.Lock()
	p.kills++
	p.mu.Unlock()
	return nil
}

func (*fakeProcRammap) IsProtected(uint32, platform.ProcInfo) bool { return false }

type fakeJobRammap struct{ terminated bool }

func (j *fakeJobRammap) Assign(uint32) error            { return nil }
func (j *fakeJobRammap) Close() error                   { return nil }
func (j *fakeJobRammap) Terminate(uint32) error         { j.terminated = true; return nil }
func (j *fakeJobRammap) SetAllowKillOnClose(bool) error { return nil }

type fakeJobAPIRammap struct{ job *fakeJobRammap }

func (a *fakeJobAPIRammap) Create() (platform.Job, error) {
	if a.job == nil {
		a.job = &fakeJobRammap{}
	}
	return a.job, nil
}

// ---------- 装配辅助 ----------

func newTestRammapService(t *testing.T) (*RAMMapService, string, *fakeProbeRammap, *fakeProcRammap) {
	t.Helper()
	versionsDir := t.TempDir()
	proc := &fakeProcRammap{}
	probe := &fakeProbeRammap{proc: proc}
	svc := &RAMMapService{
		manager:    version.NewManager(versionsDir),
		store:      newRammapStore(t.TempDir()),
		engine:     instance.NewEngine(&fakeJobAPIRammap{}, proc, probe, instance.Callbacks{}),
		holder:     extapi.NewLeaseHolder(ID),
		downloads:  map[string]struct{}{},
		isElevated: func() bool { return false },
	}
	return svc, versionsDir, probe, proc
}

// seedRammapInstalled 造 ListInstalled 可识别的最小版本目录：官方 zip 为
// 三架构平铺，此处同时铺两份载荷（x64/arm64 同级共存），本机 GOARCH 取件
// 判据完全交给生产代码，不在测试里抄载荷命名规则。
func seedRammapInstalled(t *testing.T, versionsDir, token string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "rammap_"+token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"RAMMap64.exe", "RAMMap64a.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("MZ fake payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// breakRammapPayload 把目录内两份载荷都清零字节（"下载残骸"实况）。
func breakRammapPayload(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"RAMMap64.exe", "RAMMap64a.exe"} {
		if err := os.Truncate(filepath.Join(dir, name), 0); err != nil {
			t.Fatal(err)
		}
	}
}

// ---------- 调用门拒接矩阵（Wave 3 RPC 面契约） ----------

func TestRammapGateRefusesFullRPCSurface(t *testing.T) {
	svc, _, _, _ := newTestRammapService(t)
	sentinel := errors.New("模块已停用：调用门拒接")
	svc.holder.SetGate(denyGateRammap{sentinel: sentinel})

	// 每个 RPC 的返回值形状：门拒错误必须原样上抛，不得静默消化成零值。
	if _, err := svc.ListReleases(); !errors.Is(err, sentinel) {
		t.Errorf("ListReleases 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.ListInstalledVersions(); !errors.Is(err, sentinel) {
		t.Errorf("ListInstalledVersions 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.DownloadVersion("2026-09-01"); !errors.Is(err, sentinel) {
		t.Errorf("DownloadVersion 门拒应上抛原错，实得 %v", err)
	}
	if len(svc.downloads) != 0 {
		t.Errorf("门拒路径不得在在飞名册留底，实得 %v", svc.downloads)
	}
	if _, err := svc.ImportLocal(`C:\nonexistent`); !errors.Is(err, sentinel) {
		t.Errorf("ImportLocal 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.RemoveVersion("2026-09-01"); !errors.Is(err, sentinel) {
		t.Errorf("RemoveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.SetActiveVersion("2026-09-01"); !errors.Is(err, sentinel) {
		t.Errorf("SetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetActiveVersion(); !errors.Is(err, sentinel) {
		t.Errorf("GetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetStatus(); !errors.Is(err, sentinel) {
		t.Errorf("GetStatus 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.ElevationStatus(); !errors.Is(err, sentinel) {
		t.Errorf("ElevationStatus 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.OpenWindowElevated(); !errors.Is(err, sentinel) {
		t.Errorf("OpenWindowElevated 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.OpenWindow(); !errors.Is(err, sentinel) {
		t.Errorf("OpenWindow 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.Quit(); !errors.Is(err, sentinel) {
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
	if err := svc.OpenDir(`C:\nonexistent`); !errors.Is(err, sentinel) {
		t.Errorf("OpenDir 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.OfficialSiteURL(); !errors.Is(err, sentinel) {
		t.Errorf("OfficialSiteURL 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.OpenOfficialSite(); !errors.Is(err, sentinel) {
		t.Errorf("OpenOfficialSite 门拒应上抛原错（plat 未接线，越过门即 panic），实得 %v", err)
	}
}

// ---------- 冷启动版本解析 ----------

func TestRammapResolveActiveVersionFallbackLatestDate(t *testing.T) {
	svc, versionsDir, _, _ := newTestRammapService(t)
	seedRammapInstalled(t, versionsDir, "2025-12-01")
	dir := seedRammapInstalled(t, versionsDir, "2026-09-01")

	v, exe, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "2026-09-01" {
		t.Fatalf("未指定 active 应取最新日期版，实得 %q", v)
	}
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		t.Fatalf("返回载荷路径必须真实可执行，实得 %q", exe)
	}
	if filepath.Dir(exe) != dir {
		t.Fatalf("载荷应落在最新目录 %s 内，实得 %s", dir, filepath.Dir(exe))
	}

	// 空载荷目录（下载残骸/损坏安装）不得被选为启动目标。
	broken := seedRammapInstalled(t, versionsDir, "2099-01-01")
	breakRammapPayload(t, broken)
	v2, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != "2026-09-01" {
		t.Fatalf("空载荷的 2099-01-01 不得被选中，实得 %q", v2)
	}
}

func TestRammapResolveActiveVersionSelfHealPersists(t *testing.T) {
	svc, versionsDir, _, _ := newTestRammapService(t)
	seedRammapInstalled(t, versionsDir, "2026-09-01")
	if err := svc.store.SetActive("2030-01-01"); err != nil { // active 指向不存在版本
		t.Fatal(err)
	}

	v, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "2026-09-01" {
		t.Fatalf("active 失效应回退最新已装，实得 %q", v)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("自愈须当场清空 active，实得 %q", got)
	}
	// 清空必须落盘：重启（新 store 读同一目录）不得复活坏值。
	reloaded := newRammapStore(filepath.Dir(svc.store.filePath))
	if got := reloaded.GetActive(); got != "" {
		t.Fatalf("自愈清空未持久化，重启后读到 %q", got)
	}
}

func TestRammapResolveActiveVersionPrefersSetActiveOverNewer(t *testing.T) {
	svc, versionsDir, _, _ := newTestRammapService(t)
	seedRammapInstalled(t, versionsDir, "2025-12-01")
	seedRammapInstalled(t, versionsDir, "2026-09-01")
	if err := svc.store.SetActive("2025-12-01"); err != nil {
		t.Fatal(err)
	}
	v, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "2025-12-01" {
		t.Fatalf("显式设定的 active 必须优先于更新版本（用户意志主路径），实得 %q", v)
	}
}

func TestRammapResolveActiveVersionEmptyTreeErrors(t *testing.T) {
	svc, _, _, _ := newTestRammapService(t)
	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("无任何已装版本应如实报错，实得 %v", err)
	}
}

// ---------- 提权预告投影（三重契约之③） ----------

func TestRammapElevationStatusProjection(t *testing.T) {
	svc, _, _, _ := newTestRammapService(t)
	svc.isElevated = func() bool { return true }

	st, err := svc.ElevationStatus()
	if err != nil {
		t.Fatal(err)
	}
	// 未装任何版本时仍须按 manifest 已知事实预告"需要管理员"，
	// 让 stopped 引导行如实显示提权提示，而不是探测失败就谎报不需要。
	if !st.RequiresElevation {
		t.Errorf("RAMMap 恒要求管理员，未装载荷时也必须预告提权，实得 %+v", st)
	}
	if st.ExecutionLevel != string(windows.ExecutionRequireAdministrator) {
		t.Errorf("ExecutionLevel 应预告 requireAdministrator 词表值，实得 %q", st.ExecutionLevel)
	}
	if !st.HostElevated {
		t.Error("isElevated 接缝注入 true 时 HostElevated 必须为 true")
	}
	svc.isElevated = func() bool { return false }
	st2, err := svc.ElevationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st2.HostElevated {
		t.Error("isElevated 接缝注入 false 时 HostElevated 必须为 false")
	}
}

// ---------- external 接管与退出归因 ----------

func TestRammapGetStatusAdoptsExternalInstance(t *testing.T) {
	svc, _, probe, _ := newTestRammapService(t)
	probe.set(true, &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\RAMMap64.exe`, StartedAt: time.Now()})

	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateExternal || !snap.External {
		t.Fatalf("探针在场应接管为 external，实得 %+v", snap)
	}
	if snap.PID != 4294967290 || snap.ExePath != `X:\user\RAMMap64.exe` {
		t.Fatalf("快照须回放探针实测身份（PID/ExePath），实得 %+v", snap)
	}

	// 外部实例消失后复探撤销，收敛回 stopped（重启前状态不残账）。
	probe.set(false, nil)
	snap2, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap2.State != instance.StateStopped || snap2.External {
		t.Fatalf("外部实例退出后应回 stopped，实得 %+v", snap2)
	}
}

func TestRammapQuitNotManagedMapping(t *testing.T) {
	svc, _, _, proc := newTestRammapService(t)
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "not-managed" || out.Stopped || out.Forced || out.CloseRequested {
		t.Fatalf("静止会话退出应归因 not-managed 零动作，实得 %+v", out)
	}
	if out.Message == "" {
		t.Fatal("not-managed 回执必须带用户可读说明")
	}
	if proc.killed() {
		t.Fatal("无托管实例时不得触达终止")
	}
}

func TestRammapQuitExternalUnidentifiedRefuses(t *testing.T) {
	svc, _, probe, proc := newTestRammapService(t)
	probe.set(true, nil) // 在场但身份不明（Query 全失败形态）
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "probe-missing-pid" || out.Stopped || out.Forced {
		t.Fatalf("身份不明必须拒执行并如实归因，实得 %+v", out)
	}
	if !out.External {
		t.Error("外部处置回执 External 须为 true")
	}
	if proc.killed() {
		t.Fatal("拒执行路径不得触达终止（不猜身份）")
	}
}

func TestRammapQuitExternalForceFreeForced(t *testing.T) {
	svc, _, probe, proc := newTestRammapService(t)
	info := &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\RAMMap64.exe`, StartedAt: time.Now()}
	proc.info = *info
	probe.set(true, info)
	svc.engine.RefreshExternal()
	if !svc.engine.ExternalRunning() {
		t.Fatal("前置：应已接管为 external")
	}

	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	// force-free 档：优雅不生效直接强杀不经确认——归因三元组 Forced=true、
	// CloseRequested=false（WM_CLOSE 无可投窗口）、Method=forced。
	if out.Method != "forced" || !out.Stopped || !out.Forced || out.CloseRequested {
		t.Fatalf("force-free 收口归因应 forced/Stopped/!CloseRequested，实得 %+v", out)
	}
	if !out.External {
		t.Error("外部实例处置回执 External 须为 true")
	}
	if proc.kills != 1 {
		t.Fatalf("恰一次终止调用，实得 %d", proc.kills)
	}
	// 杀后复探收口：external 撤销回 stopped，不留幽灵态。
	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateStopped {
		t.Fatalf("强杀收口后应回 stopped，实得 %s", snap.State)
	}
}

func TestRammapOpenWindowExternalFocusVerdict(t *testing.T) {
	svc, _, probe, _ := newTestRammapService(t)
	info := &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\RAMMap64.exe`, StartedAt: time.Now()}
	probe.set(true, info)

	probe.setFocus(false)
	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "external-unreachable" || !out.External || out.Managed {
		t.Fatalf("无可聚焦窗口须如实回 unreachable 指引，实得 %+v", out)
	}

	probe.setFocus(true)
	out2, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out2.Action != "external-focused" || !out2.External || out2.Managed {
		t.Fatalf("聚焦成功应回 external-focused，实得 %+v", out2)
	}
	if out2.LaunchMode != LaunchModeExternal {
		t.Fatalf("外部唤回 LaunchMode 应为 external，实得 %q", out2.LaunchMode)
	}
}

// ---------- 导入自动激活（N24 同源契约） ----------

func TestRammapImportLocalAutoActivatesOnlyWhenUnset(t *testing.T) {
	svc, _, _, _ := newTestRammapService(t)
	src := t.TempDir()
	for _, name := range []string{"RAMMap64.exe", "RAMMap64a.exe"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("MZ fake payload A"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info, err := svc.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != info.Version {
		t.Fatalf("无 active 时导入应自动激活导入结果，实得 %q 期望 %q", got, info.Version)
	}

	// 第二份内容不同的导入：用户已有设定不得被抢账。
	src2 := t.TempDir()
	for _, name := range []string{"RAMMap64.exe", "RAMMap64a.exe"} {
		if err := os.WriteFile(filepath.Join(src2, name), []byte("MZ fake payload B"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info2, err := svc.ImportLocal(src2)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.store.GetActive(); got != info.Version {
		t.Fatalf("已设 active 时导入不得覆盖用户选择，实得 %q（应为 %q，后来者 %q）", got, info.Version, info2.Version)
	}
	if info2.Version == info.Version {
		t.Fatal("前置：两份不同内容应得不同导入令牌")
	}
}
