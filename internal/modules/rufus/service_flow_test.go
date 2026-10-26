package rufus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/rufus/instance"
	"hanxi/internal/modules/rufus/version"
)

// ---------- 测试替身 ----------

// fakeRufusProbe 可编程探针：running=true 时经引擎 RefreshExternal 把静止态
// 引擎校正为 external（外部实例在场的托管侧事实），并统计 IsRunning 调用次数
// （shutdown 联动是否触探针的判据）。不依赖真实进程/互斥体。
type fakeRufusProbe struct {
	running   bool
	inspectN  atomic.Int32
	findPIDsN atomic.Int32
}

func (p *fakeRufusProbe) FindPIDs() []uint32 {
	p.findPIDsN.Add(1)
	if p.running {
		return []uint32{4242}
	}
	return nil
}
func (p *fakeRufusProbe) IsRunning() bool {
	p.inspectN.Add(1)
	return p.running
}
func (p *fakeRufusProbe) WaitForReady(time.Duration) bool { return p.running }
func (p *fakeRufusProbe) IsMainWindowOpen([]uint32) bool  { return p.running }

// toggleGate 可切换拒绝态的调用门替身（未注入门=放行；注入后由 deny 决定裁决）。
type toggleGate struct{ deny atomic.Bool }

func (g *toggleGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块已停用（测试门拒）")
	}
	return func() {}, nil
}

// ---------- 装配与资产夹具 ----------

type rufusFixture struct {
	svc         *RufusService
	probe       *fakeRufusProbe
	versionsDir string
	stateDir    string
}

func newRufusFixture(t *testing.T, running bool) *rufusFixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	probe := &fakeRufusProbe{running: running}
	holder := extapi.NewLeaseHolder(ID)
	svc := &RufusService{
		manager: version.NewManager(versionsDir),
		store:   newRufusStore(stateDir),
		holder:  holder,
		engine:  instance.NewEngine(nil, probe, instance.Callbacks{}),
	}
	return &rufusFixture{svc: svc, probe: probe, versionsDir: versionsDir, stateDir: stateDir}
}

// mkInstalled 伪造一个已落位的托管版本目录：versions/<dir>/rufus.exe 非空。
// 无 meta.json（schema=0 的迁移前历史形态），ListInstalled 走旧账本回读轨。
func mkInstalled(t *testing.T, versionsDir, dir string) string {
	t.Helper()
	d := filepath.Join(versionsDir, dir)
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(d, "rufus.exe")
	if err := os.WriteFile(exe, []byte("MZ伪造载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	return exe
}

// ---------- DownloadVersion ----------

// TestDownloadVersionInstalledDedup 下载闸的输入归一与已装去重：带/不带大小写
// v、首尾空白的同一版本，命中已装目录即返回 already-installed 且不启动任何
// 后台下载（判据：返回值与已装目录文件均未被下载链改写）。
// 注：大写 "V4.15" 形态曾归一为 "vV4.15" 致去重落空（当批登记为待裁缺陷），
// X2 批已把归一改为大小写不敏感（照 vscode normalizeVersion 样板），现如实纳入。
func TestDownloadVersionInstalledDedup(t *testing.T) {
	f := newRufusFixture(t, false)
	mkInstalled(t, f.versionsDir, "rufus_4.15")

	for _, in := range []string{"4.15", "v4.15", "V4.15", " 4.15 "} {
		got, err := f.svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（已装版本不得重复起下载）", in, got)
		}
	}
	if _, err := os.Stat(filepath.Join(f.versionsDir, "rufus_4.15")); err != nil {
		t.Fatalf("去重路径不应动盘: %v", err)
	}
}

// ---------- SetActiveVersion / GetActiveVersion ----------

// TestSetActiveVersionValidatesBeforePersisting 设定使用版本的校验闸：未安装
// 版本号被拒且既有账本原样保留；合法输入归一为 vX.Y 后返回并真实落盘
// （判据取自闭合后重新加载 store 的读值，即 rufus.json 文件内容）。
func TestSetActiveVersionValidatesBeforePersisting(t *testing.T) {
	f := newRufusFixture(t, false)
	mkInstalled(t, f.versionsDir, "rufus_4.15")
	if err := f.svc.store.SetActive("v4.9"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SetActiveVersion("v9.9"); err == nil {
		t.Fatal("未安装版本应被拒绝")
	}
	if got := newRufusStore(f.stateDir).GetActive(); got != "v4.9" {
		t.Fatalf("拒绝后账本应保持 v4.9，实得 %q", got)
	}

	got, err := f.svc.SetActiveVersion(" v4.15 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v4.15" {
		t.Fatalf("应返回归一版本 v4.15，实得 %q", got)
	}
	if reloaded := newRufusStore(f.stateDir).GetActive(); reloaded != "v4.15" {
		t.Fatalf("落盘重载应为 v4.15，实得 %q", reloaded)
	}
}

// ---------- RemoveVersion ----------

// TestRemoveVersionActiveLedgerHygiene 卸载闸的账本卫生：卸非在用版本不动
// active；卸在用版本目录消失且 active 清空（冷启动回退兜底的前提）；形状外
// 令牌（路径穿越）在触盘前被拒且不动任何目录。
func TestRemoveVersionActiveLedgerHygiene(t *testing.T) {
	f := newRufusFixture(t, false)
	mkInstalled(t, f.versionsDir, "rufus_4.15")
	mkInstalled(t, f.versionsDir, "rufus_4.14")
	if err := f.svc.store.SetActive("v4.15"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RemoveVersion("4.14"); err != nil {
		t.Fatalf("卸载未在用版本: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.versionsDir, "rufus_4.14")); !os.IsNotExist(err) {
		t.Fatal("rufus_4.14 目录应已删除")
	}
	if got := f.svc.store.GetActive(); got != "v4.15" {
		t.Fatalf("卸 v4.14 不应清 active，实得 %q", got)
	}

	if err := f.svc.RemoveVersion(" v4.15 "); err != nil {
		t.Fatalf("卸载在用版本: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.versionsDir, "rufus_4.15")); !os.IsNotExist(err) {
		t.Fatal("rufus_4.15 目录应已删除")
	}
	if got := newRufusStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("卸掉在用版本后落盘账本应清空，实得 %q", got)
	}

	// 注入形态参数在磁盘访问前被拒（resolveVersionDir 白名单），且不误删。
	if err := f.svc.RemoveVersion("../evil"); err == nil {
		t.Fatal("路径穿越形态版本应被拒绝")
	}
}

// ---------- ImportLocal ----------

// TestImportLocalFirstImportBecomesActive N24 契约 + 便携配置随行搬运：
// 首个导入版本自动成为使用版本；源旁 rufus.ini 一并搬进托管目录（内容一致）；
// 同版本重复导入被拒。判据全部取自磁盘实际文件与重载 store。
func TestImportLocalFirstImportBecomesActive(t *testing.T) {
	f := newRufusFixture(t, false)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "rufus-4.15.exe"), []byte("MZ便携载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	iniBody := "[Setting]\nUpdateCheckInterval=-1\n"
	if err := os.WriteFile(filepath.Join(src, "rufus.ini"), []byte(iniBody), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := f.svc.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if info.Version != "v4.15" || !info.IsImport {
		t.Fatalf("导入版本识别错误: %+v", info)
	}
	if got := f.svc.store.GetActive(); got != "v4.15" {
		t.Fatalf("首个导入版本应自动成为使用版本（N24），实得 %q", got)
	}
	iniPath := filepath.Join(f.versionsDir, "rufus_4.15", "rufus.ini")
	data, err := os.ReadFile(iniPath)
	if err != nil || string(data) != iniBody {
		t.Fatalf("源旁 rufus.ini 应随搬运进托管目录，实得 %q err=%v", data, err)
	}

	if _, err := f.svc.ImportLocal(src); err == nil {
		t.Fatal("同版本重复导入应被拒绝")
	}
}

// TestImportLocalRejectedWhenExternalRunning 外部实例在场（引擎经探针校正为
// external）时导入被拒，且托管目录一个字节都不新增（Windows 运行中 exe 独占，
// 拷贝必败——提前拦截而不是留半件）。
func TestImportLocalRejectedWhenExternalRunning(t *testing.T) {
	f := newRufusFixture(t, true)
	f.svc.engine.RefreshExternal()
	if snap := f.svc.engine.Snapshot(); snap.State != instance.StateExternal {
		t.Fatalf("前置：引擎应被探针校正为 external，实得 %s", snap.State)
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "rufus.exe"), []byte("MZ载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("外部实例在场应拒绝导入，实得 %v", err)
	}
	entries, _ := os.ReadDir(f.versionsDir)
	if len(entries) != 0 {
		t.Fatalf("拒绝后托管目录应为空，实有 %d 项", len(entries))
	}
}

// ---------- resolveActiveVersion / OpenWindow 冷启动解析 ----------

// TestResolveActiveVersionNeverSelectsImportedFallback 冷启动回退"最新已装"
// 的选型闸：数值降序（v4.15 > v4.9，字典序会判反）且 imported- 兜底目录即使
// 名字"排得更新"也绝不能被选中（本仓 2026-10-26 同型缺陷教训在 rufus 消费侧
// 的回归钉）；设定版本失效时清空自愈后同样回退真实最新版本。
func TestResolveActiveVersionNeverSelectsImportedFallback(t *testing.T) {
	f := newRufusFixture(t, false)
	exe15 := mkInstalled(t, f.versionsDir, "rufus_4.15")
	mkInstalled(t, f.versionsDir, "rufus_4.9")
	mkInstalled(t, f.versionsDir, "rufus_imported-20260903-120000")

	v, exe, err := f.svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v4.15" || exe != exe15 {
		t.Fatalf("未设 active 应回退数值最新 v4.15，实得 %s %s", v, exe)
	}

	if err := f.svc.store.SetActive("v9.9"); err != nil { // 已卸载/损坏的设定值
		t.Fatal(err)
	}
	v, _, err = f.svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v4.15" {
		t.Fatalf("失效自愈后应回退 v4.15，实得 %s", v)
	}
	if got := newRufusStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("失效版本应被清空自愈（落盘），实得 %q", got)
	}

	// 无任何已装时 OpenWindow 的引导文案（冷启动在解析处早退，不触进程）。
	empty := newRufusFixture(t, false)
	if _, err := empty.svc.OpenWindow(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("空态 OpenWindow 应给出引导错误，实得 %v", err)
	}
}

// ---------- Quit / Shutdown ----------

// TestQuitExternalGuidanceWithoutKill 外部实例不越权强杀：external 态 Quit
// 返回指引（Stopped=false, External=true），且引擎保持 external——未走任何
// 终止通道（判据：返回值 + 再次快照仍 external）。
func TestQuitExternalGuidanceWithoutKill(t *testing.T) {
	f := newRufusFixture(t, true)
	f.svc.engine.RefreshExternal()

	out, err := f.svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("外部实例应仅给指引不谎称已退出，实得 %+v", out)
	}
	if snap := f.svc.engine.Snapshot(); snap.State != instance.StateExternal {
		t.Fatalf("Quit 指引后外部实例应仍在场，实得 %s", snap.State)
	}
}

// TestActivateShutdownWatchChannelSemantics 后台感知的确定性生命周期：
// activate 幂等（二次调用不换通道）；shutdown 关闭停止通道（可即时收到
// 关闭读出，开放通道不会读到值——无需 sleep）；重复 shutdown 不二次 close
// （double-close 会 panic）；followOnExit=false 时 shutdown 不触引擎停止
// 通道（探针调用计数不增），true 时经 Stop 校正现场（计数增）。
func TestActivateShutdownWatchChannelSemantics(t *testing.T) {
	f := newRufusFixture(t, false)
	f.svc.activate()
	f.svc.activate()
	stop1 := f.svc.watchStop
	f.svc.activate()
	if f.svc.watchStop != stop1 {
		t.Fatal("activate 应幂等，不得替换停止通道")
	}

	inspectBefore := f.probe.inspectN.Load()
	f.svc.shutdown()
	select {
	case _, ok := <-f.svc.watchStop:
		if ok {
			t.Fatal("watchStop 只应被 close，不应有值送达")
		}
	default:
		t.Fatal("shutdown 后 watchStop 应已关闭")
	}
	if n := f.probe.inspectN.Load(); n != inspectBefore {
		t.Fatalf("followOnExit=false 的 shutdown 不应触引擎停止探针，IsRunning 调用增 %d", n-inspectBefore)
	}
	f.svc.shutdown() // 二次 shutdown：watching=false，不得 close 已关通道（panic 即红）

	// 联动开启：shutdown 经 engine.Stop 走探针校正（classify 调 IsRunning）。
	f2 := newRufusFixture(t, false)
	if err := f2.svc.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	before := f2.probe.inspectN.Load()
	f2.svc.shutdown()
	if f2.probe.inspectN.Load() <= before {
		t.Fatal("followOnExit=true 的 shutdown 应触引擎停止通道（探针被调用）")
	}
}

// ---------- OpenDir 校验分支 ----------

// TestOpenDirRejectsInvalidTargets 打开位置的入参闸：空串、不存在、文件而非
// 目录均在 explorer 拉起前被拒（成功分支会真起 explorer.exe，无判据可取且不
// 该在测试里产生桌面副作用——如实不测）。
func TestOpenDirRejectsInvalidTargets(t *testing.T) {
	f := newRufusFixture(t, false)
	if err := f.svc.OpenDir("   "); err == nil {
		t.Fatal("空目录路径应被拒")
	}
	if err := f.svc.OpenDir(filepath.Join(f.versionsDir, "不存在的目录")); err == nil {
		t.Fatal("不存在路径应被拒")
	}
	file := filepath.Join(f.versionsDir, "afile.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.OpenDir(file); err == nil || !strings.Contains(err.Error(), "不是目录") {
		t.Fatalf("文件目标应报『不是目录』，实得 %v", err)
	}
}

// ---------- Wave 3 调用门 ----------

// TestCallGateDenyRejectsEveryBusinessEntry 统一调用门接线面：门拒时全部业务
// RPC 如实上抛错误（不静默回空值），void 的 Shutdown 早退——后台轮询通道保持
// 开放不被关闭；门恢复后 Shutdown 正常收口。
func TestCallGateDenyRejectsEveryBusinessEntry(t *testing.T) {
	f := newRufusFixture(t, false)
	g := &toggleGate{deny: atomic.Bool{}}
	g.deny.Store(true)
	f.svc.holder.SetGate(g)

	if _, err := f.svc.ListInstalledVersions(); err == nil {
		t.Fatal("ListInstalledVersions 应被门拒")
	}
	if _, err := f.svc.SetActiveVersion("4.15"); err == nil {
		t.Fatal("SetActiveVersion 应被门拒")
	}
	if _, err := f.svc.GetActiveVersion(); err == nil {
		t.Fatal("GetActiveVersion 应被门拒（不得静默回空值）")
	}
	if _, err := f.svc.DownloadVersion("4.15"); err == nil {
		t.Fatal("DownloadVersion 应被门拒")
	}
	if _, err := f.svc.GetStatus(); err == nil {
		t.Fatal("GetStatus 应被门拒")
	}
	if err := f.svc.RemoveVersion("4.15"); err == nil {
		t.Fatal("RemoveVersion 应被门拒")
	}
	if err := f.svc.SetFollowOnExit(true); err == nil {
		t.Fatal("SetFollowOnExit 应被门拒")
	}

	f.svc.activate()
	f.svc.Shutdown() // void：拒即早退，不得停掉后台感知
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
