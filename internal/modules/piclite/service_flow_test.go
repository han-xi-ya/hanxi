package piclite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/piclite/instance"
	"hanxi/internal/modules/piclite/version"
)

// ---------- 测试替身 ----------

type fakePicProbe struct {
	running  bool
	inspectN atomic.Int32
}

func (p *fakePicProbe) IsRunning() bool                 { p.inspectN.Add(1); return p.running }
func (p *fakePicProbe) WaitForReady(time.Duration) bool { return p.running }
func (p *fakePicProbe) IsMainWindowOpen(uint32) bool    { return p.running }

type toggleGate struct{ deny atomic.Bool }

func (g *toggleGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块已停用（测试门拒）")
	}
	return func() {}, nil
}

type picFixture struct {
	svc         *PicLiteService
	probe       *fakePicProbe
	versionsDir string
	stateDir    string
}

func newPicFixture(t *testing.T, running bool) *picFixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	probe := &fakePicProbe{running: running}
	svc := &PicLiteService{
		manager:   version.NewManager(versionsDir),
		store:     newPicliteStore(stateDir),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	svc.engine = instance.NewEngine(nil, probe, instance.Callbacks{})
	svc.lastActivity = time.Now()
	return &picFixture{svc: svc, probe: probe, versionsDir: versionsDir, stateDir: stateDir}
}

// mkInstalled 伪造已落位版本目录：piclite_<ver>/piclite.exe 非空。
func mkInstalled(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "piclite_"+ver)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "piclite.exe"), []byte("MZ载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- DownloadVersion：去重闸与在途登记 ----------

// TestDownloadVersionAlreadyInstalledSkipsSeam 已装版本命中去重直接返回
// already-installed，下载接缝零调用（判据：返回值 + 假下载被调次数=0，
// 断先后行序使变异（放行下载）先在读计数前红）。
func TestDownloadVersionAlreadyInstalledSkipsSeam(t *testing.T) {
	f := newPicFixture(t, false)
	mkInstalled(t, f.versionsDir, "1.4.1")
	var calls atomic.Int32
	f.svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	got, err := f.svc.DownloadVersion(" v1.4.1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "already-installed" {
		t.Fatalf("已装版本应去重，实得 %q", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("去重路径不得启动下载，接缝被调 %d 次", n)
	}
}

// TestDownloadVersionInProgressDedup 同版本在途去重（确定性编排，无 sleep）：
// 首次 started，seam 阻塞在闸上期间再次调用同版本必须 in-progress 且 seam
// 仅被调 1 次；seam 收到的目标版本为归一化后的 v1.4.1（输入 " 1.4.1 "）。
// 收口后 map 清理时序不在此钉——goroutine 收口点无观察缝，用 sleep 等待即
// 违反"不得以 sleep 凑绿"红线（如实声明）。
func TestDownloadVersionInProgressDedup(t *testing.T) {
	f := newPicFixture(t, false)
	seamEntered := make(chan string, 1)
	doneAck := make(chan struct{})
	gate := make(chan struct{})
	var calls atomic.Int32
	f.svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
		calls.Add(1)
		seamEntered <- ver // 已进入 seam：在途登记必然完成（登记先于 goroutine）
		<-gate
		emit(version.DownloadProgress{Version: ver, Stage: "done"}) // 同步跑完 service 收口闭包（含 store 落账）
		close(doneAck)
		return nil
	}

	got, err := f.svc.DownloadVersion(" 1.4.1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "started" {
		t.Fatalf("首装应 started，实得 %q", got)
	}
	select {
	case v := <-seamEntered:
		if v != "v1.4.1" {
			t.Fatalf("下载目标应归一为 v1.4.1，实得 %q", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("后台下载未启动")
	}

	again, err := f.svc.DownloadVersion("v1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if again != "in-progress" {
		t.Fatalf("同版本在途中应返回 in-progress，实得 %q", again)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("在途去重应保证 seam 仅 1 次，实得 %d", n)
	}
	close(gate) // 放行收口，goroutine 正常终结（不留永久阻塞）
	select {
	case <-doneAck: // 等 store 落账收口完成后测试才结束，避免 TempDir 清理与写入竞态
	case <-time.After(3 * time.Second):
		t.Fatal("放行后后台下载未收口")
	}
}

// ---------- SetActiveVersion / RemoveVersion ----------

// TestSetActiveVersionValidatesBeforePersisting 设定使用版本的校验闸与注入
// 防护：未装被拒不改账；形状外令牌（非法字符）被拒不触盘；合法输入归一
// vX.Y.Z 后落盘（重载 store 读出为准）。
func TestSetActiveVersionValidatesBeforePersisting(t *testing.T) {
	f := newPicFixture(t, false)
	mkInstalled(t, f.versionsDir, "1.4.1")
	if err := f.svc.store.SetActive("v1.4.0"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SetActiveVersion("9.9.9"); err == nil {
		t.Fatal("未装版本应被拒")
	}
	if got := newPicliteStore(f.stateDir).GetActive(); got != "v1.4.0" {
		t.Fatalf("拒绝后账本应保持 v1.4.0，实得 %q", got)
	}
	if _, err := f.svc.SetActiveVersion("1.4.1&evil"); err == nil {
		t.Fatal("形状外版本号应被拒")
	}

	got, err := f.svc.SetActiveVersion(" v1.4.1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.4.1" {
		t.Fatalf("应返回归一版本 v1.4.1，实得 %q", got)
	}
	if reloaded := newPicliteStore(f.stateDir).GetActive(); reloaded != "v1.4.1" {
		t.Fatalf("落盘重载应为 v1.4.1，实得 %q", reloaded)
	}
}

// TestRemoveVersionClearsActiveOnTarget 卸载在用版本：目录消失且账本清空；
// 未装版本号被拒不伤及在场目录。
func TestRemoveVersionClearsActiveOnTarget(t *testing.T) {
	f := newPicFixture(t, false)
	dir := mkInstalled(t, f.versionsDir, "1.4.1")
	if err := f.svc.store.SetActive("v1.4.1"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RemoveVersion("v9.9.9"); err == nil {
		t.Fatal("未装版本卸载应被拒")
	}
	if _, err := os.Stat(filepath.Join(dir, "piclite.exe")); err != nil {
		t.Fatalf("被拒的卸载不应伤及在场目录: %v", err)
	}

	if err := f.svc.RemoveVersion(" 1.4.1 "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("版本目录应已删除")
	}
	if got := newPicliteStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("卸掉在用版本后落盘账本应清空，实得 %q", got)
	}
}

// ---------- ImportLocal ----------

// TestImportLocalGateAndN24 外部实例在场拒导入（托管目录零新增）；静止态
// 首个导入自动成为使用版本（N24），PE 版本探测失败走哈希兜底命名
// （vimported-<sha12>，同内容必同版本）。
func TestImportLocalGateAndN24(t *testing.T) {
	mkSrc := func(t *testing.T) string {
		t.Helper()
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "piclite.exe"), []byte("MZ导入载荷"), 0644); err != nil {
			t.Fatal(err)
		}
		return src
	}

	f := newPicFixture(t, true)
	f.svc.engine.RefreshExternal()
	if _, err := f.svc.ImportLocal(mkSrc(t)); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("外部实例在场应拒绝导入，实得 %v", err)
	}
	if entries, _ := os.ReadDir(f.versionsDir); len(entries) != 0 {
		t.Fatalf("拒绝后不应新增托管目录，实有 %d 项", len(entries))
	}

	f2 := newPicFixture(t, false)
	info, err := f2.svc.ImportLocal(mkSrc(t))
	if err != nil {
		t.Fatalf("静止态导入: %v", err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") || !info.IsImport {
		t.Fatalf("PE 探测失败的导入应走哈希兜底并标导入: %+v", info)
	}
	if got := f2.svc.store.GetActive(); got != info.Version {
		t.Fatalf("首个导入版本应自动成为使用版本（N24），实得 %q 期望 %q", got, info.Version)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "piclite.exe")); err != nil {
		t.Fatalf("导入 exe 应落位: %v", err)
	}

	if _, err := f2.svc.ImportLocal(mkSrc(t)); err == nil {
		t.Fatal("同内容重复导入应被拒（哈希兜底保证同版本判定）")
	}
}

// ---------- 调用门 ----------

// TestCallGateDenyRejectsBusinessEntries 门拒时业务 RPC 如实上抛、void
// Shutdown 早退不收口后台通道，门恢复后正常关闭。
func TestCallGateDenyRejectsBusinessEntries(t *testing.T) {
	f := newPicFixture(t, false)
	g := &toggleGate{}
	g.deny.Store(true)
	f.svc.holder.SetGate(g)

	if _, err := f.svc.ListInstalledVersions(); err == nil {
		t.Fatal("ListInstalledVersions 应被门拒")
	}
	if _, err := f.svc.GetActiveVersion(); err == nil {
		t.Fatal("GetActiveVersion 应被门拒（不得静默回空值）")
	}
	if _, err := f.svc.DownloadVersion("1.4.1"); err == nil {
		t.Fatal("DownloadVersion 应被门拒")
	}
	if err := f.svc.RemoveVersion("1.4.1"); err == nil {
		t.Fatal("RemoveVersion 应被门拒")
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
