package rustdesk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/rustdesk/instance"
	"hanxi/internal/modules/rustdesk/version"
)

// T2 测试加固批（RustDesk 根包）：补齐调用门拒接矩阵、双形态版本账
// （SetActiveVersion 形态词表 + GetActiveForm 成对持久化）、冷启动解析的
// 数值分段回退与损坏自愈、external 接管/唤窗/退出指引、导入运行闸与
// N24 自动激活。
//
// 生产缺陷提示（批次报告）：resolveActiveVersion 的兜底排序里
// "imported-<hash>" 剥 v 后走 versioncmp 字典序回退，'i' > '1'，导入兜底
// 目录会抢在数值版本号前被排成"最新"。本文件排序断言只用纯数值版本族，
// 不把该隐患钉死为期望。

// ---------- 测试替身 ----------

type denyGateRD struct{ sentinel error }

func (g denyGateRD) Acquire(string) (func(), error) { return nil, g.sentinel }

type fakeRDProbe struct {
	mu           sync.Mutex
	running      bool
	instancePIDs []uint32
	focusResult  int
	focusCalls   [][]uint32
}

func (p *fakeRDProbe) set(running bool, pids []uint32) {
	p.mu.Lock()
	p.running, p.instancePIDs = running, pids
	p.mu.Unlock()
}

func (p *fakeRDProbe) setFocus(n int) {
	p.mu.Lock()
	p.focusResult = n
	p.mu.Unlock()
}

func (p *fakeRDProbe) FindInstancePIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.instancePIDs...)
}

func (p *fakeRDProbe) FindOwnPIDs([]uint32) []uint32 { return nil }

func (p *fakeRDProbe) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *fakeRDProbe) HasVisibleWindow([]uint32) bool { return false }

func (p *fakeRDProbe) FocusWindows(pids []uint32) int {
	p.mu.Lock()
	p.focusCalls = append(p.focusCalls, append([]uint32(nil), pids...))
	n := p.focusResult
	p.mu.Unlock()
	return n
}

func (p *fakeRDProbe) calls() [][]uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]uint32(nil), p.focusCalls...)
}

// ---------- 装配辅助 ----------

func newTestRustDeskService(t *testing.T) (*RustDeskService, string, *fakeRDProbe) {
	t.Helper()
	versionsDir := t.TempDir()
	probe := &fakeRDProbe{}
	svc := &RustDeskService{
		manager: version.NewManager(versionsDir),
		store:   newRustDeskStore(t.TempDir()),
		engine:  instance.NewEngine(nil, probe, instance.Callbacks{}),
		holder:  extapi.NewLeaseHolder(ID),
	}
	return svc, versionsDir, probe
}

// seedRDInstalled 造隔离目录便携版本（rustdesk_<x.y.z>/rustdesk.exe 非空）。
func seedRDInstalled(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "rustdesk_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rustdesk.exe"), []byte("MZ fake payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- 冷启动解析：数值分段回退与自愈 ----------

func TestRustDeskResolveActiveVersionNumericLatest(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	older := seedRDInstalled(t, versionsDir, "1.9.0")
	newer := seedRDInstalled(t, versionsDir, "1.10.0")

	v, exe, form, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	// 字典序陷阱回归锚：v1.10.0 必须赢 v1.9.0（数值分段，非字符串比较）。
	if v != "v1.10.0" || form != version.FormPortable {
		t.Fatalf("未指定 active 应数值取最新，实得 %q form=%q", v, form)
	}
	if filepath.Dir(exe) != newer {
		t.Fatalf("载荷应取自 v1.10.0 目录 %s，实得 %s", newer, filepath.Dir(exe))
	}
	if _, serr := os.Stat(exe); serr != nil {
		t.Fatalf("返回路径须真实在场: %v", serr)
	}
	_ = older
}

func TestRustDeskResolveActiveVersionSelfHeal(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	seedRDInstalled(t, versionsDir, "1.10.0")
	if err := svc.store.SetActive("v9.9.9", version.FormPortable); err != nil {
		t.Fatal(err)
	}
	v, _, form, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v1.10.0" || form != version.FormPortable {
		t.Fatalf("坏 active 应回退最新已装便携，实得 %q/%q", v, form)
	}
	reloaded := newRustDeskStore(filepath.Dir(svc.store.filePath))
	if rv, rf := reloaded.GetActive(); rv != "" || rf != "" {
		t.Fatalf("自愈清空须落盘成对为空，实得 %q/%q", rv, rf)
	}
}

func TestRustDeskResolveActiveVersionNoPortable(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	// 便携零装：只允许两种诚实结局——报错（完全无可用）或采用系统安装版；
	// 绝不允许凭空返回 portable 空路径。注册表实况非测试可控量，两种都放行。
	v, exe, form, err := svc.resolveActiveVersion()
	if err != nil {
		return
	}
	if form != version.FormInstalled || v == "" || exe == "" {
		t.Fatalf("无便携版本时只能如实采用安装版（form/version/exe 齐备），实得 %q/%q/%q", v, form, exe)
	}
}

// ---------- SetActiveVersion 形态词表与成对持久化 ----------

func TestRustDeskSetActiveVersionFormVocabulary(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	seedRDInstalled(t, versionsDir, "1.10.0")

	// 缺省形态 = portable；入参裸号归一为 vX.Y.Z 并持久化成对。
	got, err := svc.SetActiveVersion("1.10.0", "")
	if err != nil || got != "v1.10.0" {
		t.Fatalf("已装便携版本应可设使用（缺省 portable），实得 %q err=%v", got, err)
	}
	if f, ferr := svc.GetActiveForm(); ferr != nil || f != version.FormPortable {
		t.Fatalf("GetActiveForm 应回 portable，实得 %q err=%v", f, ferr)
	}
	reloaded := newRustDeskStore(filepath.Dir(svc.store.filePath))
	if rv, rf := reloaded.GetActive(); rv != "v1.10.0" || rf != version.FormPortable {
		t.Fatalf("版本与形态须成对落盘，实得 %q/%q", rv, rf)
	}

	// 未装便携：拒设且不动账。
	if _, err := svc.SetActiveVersion("9.9.9", "portable"); err == nil {
		t.Fatal("未装版本设使用必须报错")
	}
	if rv, rf := svc.store.GetActive(); rv != "v1.10.0" || rf != version.FormPortable {
		t.Fatalf("被拒后账目不得变动，实得 %q/%q", rv, rf)
	}

	// 未知形态词表外：拒且不动账。
	if _, err := svc.SetActiveVersion("1.10.0", "weird"); err == nil || !strings.Contains(err.Error(), "未知版本形态") {
		t.Fatalf("词表外形态须如实拒绝，实得 %v", err)
	}

	// 安装版形态核验：用一个不可能与本机注册表实况吻合的版本号，
	// 无论系统装没装 RustDesk 都必须被拒（探测缺失或版本不符同话术）。
	if _, err := svc.SetActiveVersion("v99.99.99", version.FormInstalled); err == nil {
		t.Fatal("系统安装版与请求不符时必须拒设")
	}
	if rv, _ := svc.store.GetActive(); rv != "v1.10.0" {
		t.Fatalf("拒设后 active 不得变动，实得 %q", rv)
	}
}

// ---------- 下载同步回执 ----------

func TestRustDeskDownloadAlreadyInstalledSync(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	seedRDInstalled(t, versionsDir, "1.10.0")
	for _, ask := range []string{"1.10.0", "v1.10.0"} {
		if state, err := svc.DownloadVersion(ask); err != nil || state != "already-installed" {
			t.Fatalf("已装版本入参 %q 应回 already-installed，实得 %q err=%v", ask, state, err)
		}
	}
}

// ---------- 卸载与 active 联动 ----------

func TestRustDeskRemoveVersionActiveCoupling(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	d1 := seedRDInstalled(t, versionsDir, "1.9.0")
	d2 := seedRDInstalled(t, versionsDir, "1.10.0")
	if err := svc.store.SetActive("v1.10.0", version.FormPortable); err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveVersion("1.9.0"); err != nil {
		t.Fatalf("非使用中版本应放行卸载: %v", err)
	}
	if _, serr := os.Stat(d1); !os.IsNotExist(serr) {
		t.Fatalf("卸载后目录须从磁盘消失，Stat=%v", serr)
	}
	if rv, _ := svc.store.GetActive(); rv != "v1.10.0" {
		t.Fatalf("卸他版不得动 active，实得 %q", rv)
	}

	if err := svc.RemoveVersion("v1.10.0"); err != nil {
		t.Fatalf("使用中便携版本也应放行卸载（本模块无 single-guard）: %v", err)
	}
	if _, serr := os.Stat(d2); !os.IsNotExist(serr) {
		t.Fatalf("卸载后目录须从磁盘消失，Stat=%v", serr)
	}
	reloaded := newRustDeskStore(filepath.Dir(svc.store.filePath))
	if rv, rf := reloaded.GetActive(); rv != "" || rf != "" {
		t.Fatalf("卸掉使用中版本后 active/form 须成对清空并落盘，实得 %q/%q", rv, rf)
	}
}

// ---------- external 接管、唤窗与退出指引 ----------

func TestRustDeskExternalAdoptionAndOpenWindow(t *testing.T) {
	svc, _, probe := newTestRustDeskService(t)
	probe.set(true, []uint32{4242})
	probe.setFocus(2)

	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateExternal || !snap.External {
		t.Fatalf("探针在场应接管 external，实得 %+v", snap)
	}

	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "external-opened" || !out.External {
		t.Fatalf("有窗可唤应回 external-opened，实得 %+v", out)
	}
	if !strings.Contains(out.Message, "2") {
		t.Fatalf("回执须如实报命中窗口数，实得 %q", out.Message)
	}
	calls := probe.calls()
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != 4242 {
		t.Fatalf("唤窗必须按探针实测外部实例 PID 集操作，实得 %v", calls)
	}

	// 驻留托盘无窗：只给指引，绝不越权拉起新实例（不触 Start/SpawnWindow）。
	probe.setFocus(0)
	out2, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out2.Action != "external" || !out2.External {
		t.Fatalf("无窗可唤应回 external 指引，实得 %+v", out2)
	}

	// 外部实例退出后复探收敛。
	probe.set(false, nil)
	snap2, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap2.State != instance.StateStopped {
		t.Fatalf("外部退出后应回 stopped，实得 %s", snap2.State)
	}
}

func TestRustDeskQuitExternalGuidanceOnly(t *testing.T) {
	svc, _, probe := newTestRustDeskService(t)
	probe.set(true, []uint32{4242})
	if _, err := svc.GetStatus(); err != nil { // Quit 读快照不复探，先经 GetStatus 建立 external 事实
		t.Fatal(err)
	}
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("外部实例不越权强杀，实得 %+v", out)
	}
	if out.Message == "" {
		t.Fatal("指引文案不得为空")
	}
}

func TestRustDeskQuitStoppedIdempotent(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || out.External {
		t.Fatalf("静止会话退出应幂等成功（已退出口径），实得 %+v", out)
	}
}

// ---------- 导入运行闸与 N24 自动激活 ----------

func writeImportExe(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append([]byte("MZ"), []byte(" fake rustdesk portable bytes")...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRustDeskImportLocalRefusedWhileExternal(t *testing.T) {
	svc, _, probe := newTestRustDeskService(t)
	probe.set(true, []uint32{4242})
	if _, err := svc.GetStatus(); err != nil { // 先接管为 external（ImportLocal 判据读快照）
		t.Fatal(err)
	}
	src := writeImportExe(t, "rustdesk-1.2.9-x86_64.exe")
	if _, err := svc.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("运行中导入必须被拒（exe 独占）, 实得 %v", err)
	}
	if v, _ := svc.store.GetActive(); v != "" {
		t.Fatalf("被拒导入不得落账，实得 %q", v)
	}
}

func TestRustDeskImportLocalN24AutoActivate(t *testing.T) {
	svc, versionsDir, probe := newTestRustDeskService(t)
	probe.set(false, nil)
	src := writeImportExe(t, "rustdesk-1.2.9-x86_64.exe")
	info, err := svc.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.2.9" {
		t.Fatalf("官方便携命名应解析出版本 v1.2.9，实得 %q", info.Version)
	}
	if fi, serr := os.Stat(filepath.Join(info.Dir, "rustdesk.exe")); serr != nil || fi.Size() == 0 {
		t.Fatalf("导入须把 exe 收进隔离目录实位，Stat=%v", serr)
	}
	if v, f := svc.store.GetActive(); v != "v1.2.9" || f != version.FormPortable {
		t.Fatalf("零配置导入应自动激活 N24，实得 %q/%q", v, f)
	}

	// 再导入不同版本：已有设定不得被抢账。
	src2 := writeImportExe(t, "rustdesk-1.2.10-x86_64.exe")
	info2, err := svc.ImportLocal(src2)
	if err != nil {
		t.Fatal(err)
	}
	if info2.Version == info.Version {
		t.Fatal("前置：不同版本名应得不同令牌")
	}
	if v, _ := svc.store.GetActive(); v != "v1.2.9" {
		t.Fatalf("二次导入不得覆盖用户选择，实得 %q", v)
	}
	// 两版并立可见且形态如实标 portable（磁盘账实核对）。
	installed, ierr := svc.manager.ListInstalled()
	if ierr != nil || len(installed) != 2 {
		t.Fatalf("两次导入都应入账可见，实得 %+v err=%v", installed, ierr)
	}
	for _, it := range installed {
		if it.Form != version.FormPortable {
			t.Fatalf("导入件应标 portable 形态，实得 %q/%q", it.Version, it.Form)
		}
	}
	_ = versionsDir
}

// ---------- 调用门拒接矩阵 ----------

func TestRustDeskGateRefusesRPCSurface(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	sentinel := errors.New("模块已停用：调用门拒接")
	svc.holder.SetGate(denyGateRD{sentinel: sentinel})

	checks := []struct {
		name string
		call func() error
	}{
		{"ListReleases", func() error { _, err := svc.ListReleases(); return err }},
		{"ListInstalledVersions", func() error { _, err := svc.ListInstalledVersions(); return err }},
		{"DownloadVersion", func() error { _, err := svc.DownloadVersion("1.2.9"); return err }},
		{"InstallVersion", func() error { _, err := svc.InstallVersion("1.2.9"); return err }},
		{"RemoveVersion", func() error { return svc.RemoveVersion("1.2.9") }},
		{"SetActiveVersion", func() error { _, err := svc.SetActiveVersion("1.2.9", "portable"); return err }},
		{"GetActiveVersion", func() error { _, err := svc.GetActiveVersion(); return err }},
		{"GetActiveForm", func() error { _, err := svc.GetActiveForm(); return err }},
		{"ImportLocal", func() error { _, err := svc.ImportLocal(`C:\nonexistent.exe`); return err }},
		{"GetStatus", func() error { _, err := svc.GetStatus(); return err }},
		{"OpenWindow", func() error { _, err := svc.OpenWindow(); return err }},
		{"Quit", func() error { _, err := svc.Quit(); return err }},
		{"Shutdown", func() error { return svc.Shutdown() }},
		{"OpenDir", func() error { return svc.OpenDir(`C:\nonexistent`) }},
		{"OpenConfigDir", func() error { return svc.OpenConfigDir() }},
		{"CreateDesktopShortcut", func() error { return svc.CreateDesktopShortcut() }},
		{"OpenUninstallSettings", func() error { return svc.OpenUninstallSettings() }},
		{"RepositoryURL", func() error { _, err := svc.RepositoryURL(); return err }},
		{"OpenRepository", func() error { return svc.OpenRepository() }},
		{"GetFollowOnExit", func() error { _, err := svc.GetFollowOnExit(); return err }},
		{"SetFollowOnExit", func() error { return svc.SetFollowOnExit(true) }},
	}
	for _, c := range checks {
		if err := c.call(); !errors.Is(err, sentinel) {
			t.Errorf("%s 门拒应原样上抛调用门错误，实得 %v", c.name, err)
		}
	}
	// 门拒必须早于一切副作用：版本账与偏好账都不得动。
	if v, f := svc.store.GetActive(); v != "" || f != "" {
		t.Errorf("门拒期间 active 不得变动，实得 %q/%q", v, f)
	}
	if svc.store.GetFollowOnExit() {
		t.Error("门拒期间 followOnExit 不得被写 true")
	}
}
