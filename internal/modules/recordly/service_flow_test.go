package recordly

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/recordly/instance"
	"hanxi/internal/modules/recordly/version"
)

// ---------- 测试替身 ----------

// fakeRecordlyProbe 可编程探针：running=true 时经 RefreshExternal 把静止态
// 校正为 external；统计 IsRunning 调用次数。
type fakeRecordlyProbe struct {
	running  bool
	inspectN atomic.Int32
}

func (p *fakeRecordlyProbe) IsRunning() bool                 { p.inspectN.Add(1); return p.running }
func (p *fakeRecordlyProbe) WaitForReady(time.Duration) bool { return p.running }
func (p *fakeRecordlyProbe) IsMainWindowOpen() bool          { return p.running }

type toggleGate struct{ deny atomic.Bool }

func (g *toggleGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块已停用（测试门拒）")
	}
	return func() {}, nil
}

// ---------- 装配与资产夹具 ----------

type recordlyFixture struct {
	svc         *RecordlyService
	probe       *fakeRecordlyProbe
	versionsDir string
	stateDir    string
}

func newRecordlyFixture(t *testing.T, running bool) *recordlyFixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	probe := &fakeRecordlyProbe{running: running}
	svc := &RecordlyService{
		manager: version.NewManager(versionsDir),
		store:   newRecordlyStore(stateDir),
		holder:  extapi.NewLeaseHolder(ID),
		engine:  instance.NewEngine(nil, probe, instance.Callbacks{}),
	}
	svc.lastActivity = time.Now()
	return &recordlyFixture{svc: svc, probe: probe, versionsDir: versionsDir, stateDir: stateDir}
}

// mkInstall 在 versions/recordly 伪造合法 Electron 安装（Recordly.exe 非空 +
// resources/app.asar），可选写 hanxi-meta.json 的 tag（beta 版本标签形态）。
func mkInstall(t *testing.T, versionsDir, tag string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "recordly")
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Recordly.exe"), []byte("MZ安装载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "app.asar"), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	if tag != "" {
		meta := `{"tag":"` + tag + `"}`
		if err := os.WriteFile(filepath.Join(dir, "hanxi-meta.json"), []byte(meta), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ---------- DownloadVersion：beta tag 与数值请求互认去重 ----------

// TestDownloadVersionBetaTagCoreDedup 已装 beta 标签版（hanxi-meta tag
// v1.3.5-beta.2，PE 版本探测无法承载预发布后缀）时，按数值核心请求 v1.3.5
// 命中 CompareCore 互认直接返回 already-installed——不重复起 NSIS 下载；
// 判据：返回值 + 托管安装目录文件未被触碰。
func TestDownloadVersionBetaTagCoreDedup(t *testing.T) {
	f := newRecordlyFixture(t, false)
	dir := mkInstall(t, f.versionsDir, "v1.3.5-beta.2")

	for _, in := range []string{"1.3.5", "v1.3.5", " v1.3.5 "} {
		got, err := f.svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（beta tag 应与管理版本数值核心互认）", in, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Recordly.exe")); err != nil {
		t.Fatalf("去重路径不应动盘: %v", err)
	}
}

// ---------- RemoveVersion：运行保护不按参数放行 ----------

// TestRemoveVersionAnyRunningFormRejects 单安装目录的运行保护与请求参数无关：
// 外部实例在场（引擎校正 external）时，传不匹配版本号也拒——旧实现按参数比对
// 保护、按固定目录删除，传任意版本即可绕过（BUG-024 变体），本用例钉死 service
// 层这道闸；判据：错误文案 + 安装目录文件完好。
func TestRemoveVersionAnyRunningFormRejects(t *testing.T) {
	f := newRecordlyFixture(t, true)
	dir := mkInstall(t, f.versionsDir, "v1.3.5")
	f.svc.engine.RefreshExternal()
	if snap := f.svc.engine.Snapshot(); snap.State != instance.StateExternal {
		t.Fatalf("前置：应被校正为 external，实得 %s", snap.State)
	}

	if err := f.svc.RemoveVersion("v9.9.9"); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("外部实例在场时任何版本号都应拒绝卸载，实得 %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Recordly.exe")); err != nil {
		t.Fatalf("拒绝后安装目录应完好: %v", err)
	}
}

// TestRemoveVersionStoppedMismatchGuard 静止态下 manager 二次核对兜底：请求
// 版本与实装核心不一致时拒绝，目录不动。
func TestRemoveVersionStoppedMismatchGuard(t *testing.T) {
	f := newRecordlyFixture(t, false)
	dir := mkInstall(t, f.versionsDir, "v1.3.5")
	if err := f.svc.RemoveVersion("v2.0.0"); err == nil {
		t.Fatal("不匹配版本号的卸载应被拒")
	}
	if _, err := os.Stat(filepath.Join(dir, "Recordly.exe")); err != nil {
		t.Fatalf("拒绝后安装目录应完好: %v", err)
	}
}

// ---------- 更新通道闸 ----------

// TestSetReleaseChannelEchoAndPersist 通道切换闸：合法值回显并真实落盘
// （判据：以同目录重载 store 读出）；非法值被拒且现值不动。
func TestSetReleaseChannelEchoAndPersist(t *testing.T) {
	f := newRecordlyFixture(t, false)

	got, err := f.svc.SetReleaseChannel(ChannelBeta)
	if err != nil {
		t.Fatal(err)
	}
	if got != ChannelBeta {
		t.Fatalf("成功切换应回显 beta，实得 %q", got)
	}
	if reloaded := newRecordlyStore(f.stateDir).GetReleaseChannel(); reloaded != ChannelBeta {
		t.Fatalf("通道切换应落盘（重载读出 beta），实得 %q", reloaded)
	}

	if _, err := f.svc.SetReleaseChannel("nightly"); err == nil {
		t.Fatal("非法通道应被拒")
	}
	if got := newRecordlyStore(f.stateDir).GetReleaseChannel(); got != ChannelBeta {
		t.Fatalf("非法切换不应破坏现值，实得 %q", got)
	}
}

// ---------- ImportLocal 闸与布局 ----------

// mkElectronSource 伪造可导入的 Recordly 安装目录（Recordly.exe + resources/app.asar）。
func mkElectronSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Recordly.exe"), []byte("MZ导入载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "resources", "app.asar"), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	return src
}

// TestImportLocalGatesOccupiedAndMissingLayout 导入三道闸：外部实例在场拒且
// 托管目录零新增；缺 app.asar 的源目录拒（不是 Recordly 安装目录）；已有托管
// 安装时再导入拒（需先卸载）。判据：错误文案 + versions 目录实际内容。
func TestImportLocalGatesOccupiedAndMissingLayout(t *testing.T) {
	f := newRecordlyFixture(t, true)
	f.svc.engine.RefreshExternal()
	if _, err := f.svc.ImportLocal(mkElectronSource(t)); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("外部实例在场应拒绝导入，实得 %v", err)
	}
	if entries, _ := os.ReadDir(f.versionsDir); len(entries) != 0 {
		t.Fatalf("拒绝后不应新增托管目录，实有 %d 项", len(entries))
	}

	f2 := newRecordlyFixture(t, false)
	badSrc := t.TempDir() // 空目录：缺 Recordly.exe
	if _, err := f2.svc.ImportLocal(badSrc); err == nil || !strings.Contains(err.Error(), "Recordly.exe") {
		t.Fatalf("缺主程序的源应拒并点名缺失项，实得 %v", err)
	}

	f3 := newRecordlyFixture(t, false)
	if _, err := f3.svc.ImportLocal(mkElectronSource(t)); err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if _, err := f3.svc.ImportLocal(mkElectronSource(t)); err == nil || !strings.Contains(err.Error(), "请先") {
		t.Fatalf("托管目录已有版本时应拒（需先卸载），实得 %v", err)
	}
}

// ---------- OpenWindow 空态引导 ----------

// TestOpenWindowEmptyGuides 空态冷启动在解析处早退给出引导（不触进程、
// 不开下载）；判据错误文案。
func TestOpenWindowEmptyGuides(t *testing.T) {
	f := newRecordlyFixture(t, false)
	if _, err := f.svc.OpenWindow(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("空态应给引导错误，实得 %v", err)
	}
}

// ---------- Quit 外部指引 ----------

// TestQuitExternalGuidanceWithoutKill 外部实例不越权强杀：external 态仅给指引。
func TestQuitExternalGuidanceWithoutKill(t *testing.T) {
	f := newRecordlyFixture(t, true)
	f.svc.engine.RefreshExternal()
	out, err := f.svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("外部实例应仅给指引不谎称已退出，实得 %+v", out)
	}
}

// ---------- 后台感知生命周期与调用门 ----------

// TestActivateShutdownWatchChannelSemantics activate 幂等、shutdown 关闭
// 停止通道（关闭判据取自通道读出，无 sleep）、重复 shutdown 不二次 close；
// followOnExit=false 不触引擎停止探针、true 时触达。
func TestActivateShutdownWatchChannelSemantics(t *testing.T) {
	f := newRecordlyFixture(t, false)
	f.svc.activate()
	stop1 := f.svc.watchStop
	f.svc.activate()
	if f.svc.watchStop != stop1 {
		t.Fatal("activate 应幂等，不得替换停止通道")
	}

	before := f.probe.inspectN.Load()
	f.svc.shutdown()
	select {
	case _, ok := <-f.svc.watchStop:
		if ok {
			t.Fatal("watchStop 只应被 close")
		}
	default:
		t.Fatal("shutdown 后 watchStop 应已关闭")
	}
	if n := f.probe.inspectN.Load(); n != before {
		t.Fatalf("followOnExit=false 的 shutdown 不应触探针，IsRunning 调用增 %d", n-before)
	}
	f.svc.shutdown() // 不得 double-close（panic 即红）

	f2 := newRecordlyFixture(t, false)
	if err := f2.svc.store.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	before2 := f2.probe.inspectN.Load()
	f2.svc.shutdown()
	if f2.probe.inspectN.Load() <= before2 {
		t.Fatal("followOnExit=true 的 shutdown 应触引擎停止通道（探针被调用）")
	}
}

// TestCallGateDenyRejectsBusinessEntries 门拒时业务 RPC 全部如实上抛；
// void Shutdown 拒即早退，门恢复后正常收口。
func TestCallGateDenyRejectsBusinessEntries(t *testing.T) {
	f := newRecordlyFixture(t, false)
	g := &toggleGate{}
	g.deny.Store(true)
	f.svc.holder.SetGate(g)

	if _, err := f.svc.GetReleaseChannel(); err == nil {
		t.Fatal("GetReleaseChannel 应被门拒")
	}
	if _, err := f.svc.ListInstalledVersions(); err == nil {
		t.Fatal("ListInstalledVersions 应被门拒")
	}
	if _, err := f.svc.SetReleaseChannel(ChannelBeta); err == nil {
		t.Fatal("SetReleaseChannel 应被门拒")
	}
	if err := f.svc.RemoveVersion("v1.3.5"); err == nil {
		t.Fatal("RemoveVersion 应被门拒")
	}
	if _, err := f.svc.DownloadVersion("v1.3.5"); err == nil {
		t.Fatal("DownloadVersion 应被门拒")
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
	f.svc.Shutdown()
	select {
	case _, ok := <-f.svc.watchStop:
		if ok {
			t.Fatal("watchStop 只应被 close")
		}
	default:
		t.Fatal("门恢复后 Shutdown 应关闭停止通道")
	}
}
