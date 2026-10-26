package vscode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/vscode/instance"
	"hanxi/internal/modules/vscode/version"
)

// ---------- 测试替身 ----------

// fakeVSProbe 按形态可编程探针：running=true 时把静止态引擎校正为 external，
// 并统计 IsRunning 调用（形态路由的行为侧证：走错引擎探针计数不涨）。
type fakeVSProbe struct {
	running  bool
	inspectN atomic.Int32
}

func (p *fakeVSProbe) IsRunning() bool                 { p.inspectN.Add(1); return p.running }
func (p *fakeVSProbe) WaitForReady(time.Duration) bool { return p.running }

type toggleGate struct{ deny atomic.Bool }

func (g *toggleGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块已停用（测试门拒）")
	}
	return func() {}, nil
}

// ---------- 双形态装配与资产夹具 ----------

type vscodeFixture struct {
	svc         *VSCodeService
	portable    *fakeVSProbe
	installer   *fakeVSProbe
	versionsDir string
	stateDir    string
}

func newVSCodeFixture(t *testing.T, portableRunning, installerRunning bool) *vscodeFixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	p, i := &fakeVSProbe{running: portableRunning}, &fakeVSProbe{running: installerRunning}
	svc := &VSCodeService{
		manager: version.NewManager(versionsDir),
		store:   newVSCodeStore(stateDir),
		holder:  extapi.NewLeaseHolder(ID),
	}
	svc.portableEngine = instance.NewEngine(nil, p, instance.Callbacks{})
	svc.installerEngine = instance.NewEngine(nil, i, instance.Callbacks{})
	return &vscodeFixture{svc: svc, portable: p, installer: i, versionsDir: versionsDir, stateDir: stateDir}
}

// mkPortable 伪造合法便携布局版本目录：Code.exe 非空 + bin/code.cmd（ListInstalled
// 布局闸的最小充分形状）。
func mkPortable(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "vscode_"+ver)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Code.exe"), []byte("MZ载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "code.cmd"), []byte("@echo off"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- DownloadVersion ----------

// TestDownloadVersionPortableDedupAndFormFallback 便携通道去重闸：归一化
// （剥 v/V、去空白）命中已装返回 already-installed，不启动后台下载；未知形态
// 字符串必须归入便携通道（若误落安装通道会绕开此处去重直接起安装 goroutine）。
func TestDownloadVersionPortableDedupAndFormFallback(t *testing.T) {
	f := newVSCodeFixture(t, false, false)
	mkPortable(t, f.versionsDir, "1.106.0")

	for _, c := range []struct{ ver, form string }{
		{"v1.106.0", "portable"},
		{" 1.106.0 ", "PORTABLE"},
		{"V1.106.0", "完全不认识的形态"},
	} {
		out, err := f.svc.DownloadVersion(c.ver, c.form, false)
		if err != nil {
			t.Fatalf("DownloadVersion(%q,%q): %v", c.ver, c.form, err)
		}
		if out.Action != "already-installed" {
			t.Fatalf("DownloadVersion(%q,%q) action=%q，期望 already-installed（%q 不得走安装通道重复下载）", c.ver, c.form, out.Action, c.form)
		}
	}
}

// ---------- RemoveVersion / SetActiveVersion ----------

// TestRemoveVersionActiveLedgerHygiene 卸载闸账本卫生与注入防护：卸非在用版
// 本不动 active；卸在用版本目录消失且账本清空；路径穿越令牌在触盘前被拒、
// 不伤及任何已装目录。
func TestRemoveVersionActiveLedgerHygiene(t *testing.T) {
	f := newVSCodeFixture(t, false, false)
	dir106 := mkPortable(t, f.versionsDir, "1.106.0")
	dir105 := mkPortable(t, f.versionsDir, "1.105.0")
	if err := f.svc.store.SetActive("1.106.0"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RemoveVersion("1.105.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir105); !os.IsNotExist(err) {
		t.Fatal("1.105.0 目录应已删除")
	}
	if got := f.svc.store.GetActive(); got != "1.106.0" {
		t.Fatalf("卸非在用版本不应动 active，实得 %q", got)
	}

	if err := f.svc.RemoveVersion("V1.106.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir106); !os.IsNotExist(err) {
		t.Fatal("1.106.0 目录应已删除")
	}
	if got := newVSCodeStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("卸掉在用版本后落盘账本应清空，实得 %q", got)
	}

	mkPortable(t, f.versionsDir, "1.104.0")
	if err := f.svc.RemoveVersion("../../../evil"); err == nil {
		t.Fatal("路径穿越形态应被拒")
	}
	if _, err := os.Stat(filepath.Join(f.versionsDir, "vscode_1.104.0")); err != nil {
		t.Fatalf("被拒的卸载不应伤及其他版本目录: %v", err)
	}
}

// TestSetActiveVersionValidatesBeforePersisting 先校验已装再落账：未装版本
// 被拒且旧账本原样；合法版本剥 v 归一后返回并落盘（判据：重载 store）。
func TestSetActiveVersionValidatesBeforePersisting(t *testing.T) {
	f := newVSCodeFixture(t, false, false)
	mkPortable(t, f.versionsDir, "1.106.0")
	if err := f.svc.store.SetActive("1.105.0"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SetActiveVersion("9.9.9"); err == nil {
		t.Fatal("未安装版本应被拒绝")
	}
	if got := newVSCodeStore(f.stateDir).GetActive(); got != "1.105.0" {
		t.Fatalf("拒绝后账本不应被改写，实得 %q", got)
	}

	got, err := f.svc.SetActiveVersion(" v1.106.0 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.106.0" {
		t.Fatalf("应返回剥 v 的归一版本，实得 %q", got)
	}
	if reloaded := newVSCodeStore(f.stateDir).GetActive(); reloaded != "1.106.0" {
		t.Fatalf("落盘重载应为 1.106.0，实得 %q", reloaded)
	}
}

// ---------- resolveStartTarget ----------

// TestResolveStartTargetSelfHealAndNumericLatest 冷启动目标解析：失效设定
// 清空自愈（落盘）后回退数值最新（1.106.0 > 1.100.0 > 1.99.0，字典序会把
// 1.99.0 判最大——必须数值分段）；无任何已装时如实报引导错误。
func TestResolveStartTargetSelfHealAndNumericLatest(t *testing.T) {
	f := newVSCodeFixture(t, false, false)
	dir106 := mkPortable(t, f.versionsDir, "1.106.0")
	mkPortable(t, f.versionsDir, "1.100.0")
	mkPortable(t, f.versionsDir, "1.99.0")
	if err := f.svc.store.SetActive("1.999.0"); err != nil { // 已被卸载的设定值
		t.Fatal(err)
	}

	ver, exe, err := f.svc.resolveStartTarget(false)
	if err != nil {
		t.Fatal(err)
	}
	if ver != "1.106.0" || exe != filepath.Join(dir106, "Code.exe") {
		t.Fatalf("应回退数值最新 1.106.0，实得 %s %s", ver, exe)
	}
	if got := newVSCodeStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("失效设定应已清空自愈（落盘），实得 %q", got)
	}

	empty := newVSCodeFixture(t, false, false)
	if _, _, err := empty.svc.resolveStartTarget(false); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("空态应给引导错误，实得 %v", err)
	}
}

// ---------- 双形态互不串账 ----------

// TestQuitFormRoutingIsolation 两形态引擎路由互不串账：便携 external 时
// Quit("installer") 走安装版引擎（自有通道幂等成功 Stopped=true），
// Quit("portable") 才拿到 external 指引；空白/大小写形态归一同 installer；
// 未知形态归便携。判据取返回值组合——路由错引擎时两组结果必然互换。
func TestQuitFormRoutingIsolation(t *testing.T) {
	f := newVSCodeFixture(t, true, false)
	f.svc.portableEngine.RefreshExternal()
	if snap := f.svc.portableEngine.Snapshot(); snap.State != instance.StateExternal {
		t.Fatalf("前置：便携引擎应被校正为 external，实得 %s", snap.State)
	}

	out, err := f.svc.Quit("installer")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || out.External {
		t.Fatalf("Quit(installer) 应走安装版引擎幂等成功，实得 %+v（若此处 External=true 即串账到便携引擎）", out)
	}
	if out, err := f.svc.Quit("  INSTALLER  "); err != nil || !out.Stopped || out.External {
		t.Fatalf("形态归一（大小写+空白）应保持安装版通道，实得 %+v %v", out, err)
	}

	out, err = f.svc.Quit("portable")
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("Quit(portable) 应得到外部实例指引（不越权强杀），实得 %+v", out)
	}
	if out, err := f.svc.Quit("随便什么形态"); err != nil || out.Stopped || !out.External {
		t.Fatalf("未知形态应归便携通道，实得 %+v %v", out, err)
	}
}

// ---------- ImportLocal ----------

// mkImportSource 伪造可导入的便携源目录：Code.exe + bin/code.cmd + data/ 旧数据。
func mkImportSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Code.exe"), []byte("MZ导入载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "code.cmd"), []byte("@echo off"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data", "旧会话.txt"), []byte("陈旧数据"), 0644); err != nil {
		t.Fatal(err)
	}
	return src
}

// TestImportLocalGatesAndN24 导入闸三连：便携 external 拒（不新增目录）；安装
// 版 external 也拒（目录可能被占用）；静止态成功——首导自动立使用版本（N24），
// 导入即新环境（data\ 重建但源内旧数据绝不带入），安装版形态源（含 unins000.exe）
// 拒收。判据全部取磁盘实际目录内容。
func TestImportLocalGatesAndN24(t *testing.T) {
	// 便携通道在场即拒
	f := newVSCodeFixture(t, true, false)
	f.svc.portableEngine.RefreshExternal()
	if _, err := f.svc.ImportLocal(mkImportSource(t)); err == nil || !strings.Contains(err.Error(), "便携版实例正在运行") {
		t.Fatalf("便携实例在场应拒绝导入，实得 %v", err)
	}
	if entries, _ := os.ReadDir(f.versionsDir); len(entries) != 0 {
		t.Fatalf("拒绝后不应新增托管目录，实有 %d 项", len(entries))
	}

	// 安装版通道在场也拒
	f2 := newVSCodeFixture(t, false, true)
	f2.svc.installerEngine.RefreshExternal()
	if _, err := f2.svc.ImportLocal(mkImportSource(t)); err == nil || !strings.Contains(err.Error(), "安装版实例正在运行") {
		t.Fatalf("安装版实例在场应拒绝导入，实得 %v", err)
	}

	// 静止态导入成功 + N24 + data 隔离
	f3 := newVSCodeFixture(t, false, false)
	info, err := f3.svc.ImportLocal(mkImportSource(t))
	if err != nil {
		t.Fatalf("静止态导入: %v", err)
	}
	if !info.IsImport {
		t.Fatalf("导入项应标 IsImport: %+v", info)
	}
	if got := f3.svc.store.GetActive(); got != info.Version {
		t.Fatalf("首个导入版本应自动成为使用版本（N24），实得 %q 期望 %q", got, info.Version)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "data")); err != nil {
		t.Fatalf("导入应重建 data\\ 激活器: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "data", "旧会话.txt")); !os.IsNotExist(err) {
		t.Fatal("导入即新环境：源 data\\ 旧数据绝不应被带进托管目录")
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "Code.exe")); err != nil {
		t.Fatalf("主程序应落位: %v", err)
	}

	// 安装版目录（含卸载器）拒收
	srcInstaller := mkImportSource(t)
	if err := os.WriteFile(filepath.Join(srcInstaller, "unins000.exe"), []byte("MZ卸载器"), 0644); err != nil {
		t.Fatal(err)
	}
	f4 := newVSCodeFixture(t, false, false)
	if _, err := f4.svc.ImportLocal(srcInstaller); err == nil || !strings.Contains(err.Error(), "安装版") {
		t.Fatalf("安装版目录应拒收，实得 %v", err)
	}
}

// ---------- Wave 3 调用门 ----------

// TestCallGateDenyRejectsBusinessEntries 门拒时业务 RPC 全部如实上抛；
// OpenRepository（plat 通道）被拒时不得触达平台层（plat=nil，触达即 panic——
// 以"不 panic + 返回错误"为外部判据）。
func TestCallGateDenyRejectsBusinessEntries(t *testing.T) {
	f := newVSCodeFixture(t, false, false)
	g := &toggleGate{}
	g.deny.Store(true)
	f.svc.holder.SetGate(g)

	if _, err := f.svc.ListInstalledVersions(); err == nil {
		t.Fatal("ListInstalledVersions 应被门拒")
	}
	if _, err := f.svc.SetActiveVersion("1.106.0"); err == nil {
		t.Fatal("SetActiveVersion 应被门拒")
	}
	if _, err := f.svc.GetActiveVersion(); err == nil {
		t.Fatal("GetActiveVersion 应被门拒（不得静默回空值）")
	}
	if err := f.svc.SetFollowOnExit(true); err == nil {
		t.Fatal("SetFollowOnExit 应被门拒")
	}
	f.svc.activate()
	f.svc.Shutdown()
	select {
	case _, ok := <-f.svc.watchStop:
		_ = ok
		t.Fatal("门拒时 Shutdown 应早退，watchStop 不应关闭")
	default:
	}
	g.deny.Store(false)
	if err := f.svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-f.svc.watchStop:
		if ok {
			t.Fatal("watchStop 只应被 close")
		}
	default:
		t.Fatal("门恢复后 Shutdown 应关闭停止通道")
	}
}
