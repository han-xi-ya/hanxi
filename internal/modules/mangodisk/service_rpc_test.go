package mangodisk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/mangodisk/instance"
	"hanxi/internal/modules/mangodisk/version"
	"hanxi/internal/platform"
)

// T2 测试加固批（MangoDisk 根包）：下载单飞闸（TryLock 冲突拒语与锁不残留）、
// 已装同步回执、完整性闸门在激活与冷启动处的真实拦截、external 接管后
// 导入/退出/唤窗的处置、调用门拒接矩阵。
//
// 本模块是六兄弟中唯一"取最新"排序正确的样板（resolveActiveVersion 先剥
// v 前缀再交 versioncmp，且令牌恒为 x.y.z 形状），但根包测试无法造出通过
// PE 身份核验的假资产，故排序正例放在 version 包既有测试族，本文件钉死
// 的是"完整性无效资产绝不被选为启动目标"这一拦截面。

// ---------- 测试替身 ----------

type denyGateMD struct{ sentinel error }

func (g denyGateMD) Acquire(string) (func(), error) { return nil, g.sentinel }

type fakeMDProbe struct {
	mu        sync.Mutex
	running   bool
	signalPID uint32
}

func (p *fakeMDProbe) set(running bool, pid uint32) {
	p.mu.Lock()
	p.running, p.signalPID = running, pid
	p.mu.Unlock()
}

func (p *fakeMDProbe) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *fakeMDProbe) WaitForReady(time.Duration) bool { return true }

func (p *fakeMDProbe) SignalPID() (uint32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.signalPID, p.signalPID != 0
}

type fakeMDJob struct{}

func (fakeMDJob) Assign(uint32) error            { return nil }
func (fakeMDJob) Close() error                   { return nil }
func (fakeMDJob) Terminate(uint32) error         { return nil }
func (fakeMDJob) SetAllowKillOnClose(bool) error { return nil }

type fakeMDJobAPI struct{}

func (fakeMDJobAPI) Create() (platform.Job, error) { return fakeMDJob{}, nil }

// ---------- 装配辅助 ----------

func newTestMangoDiskService(t *testing.T) (*MangoDiskService, string, *fakeMDProbe) {
	t.Helper()
	versionsDir := t.TempDir()
	probe := &fakeMDProbe{}
	svc := &MangoDiskService{
		manager: version.NewManager(versionsDir),
		store:   newMangoDiskStore(t.TempDir()),
		engine:  instance.NewEngine(fakeMDJobAPI{}, probe, instance.Callbacks{}),
		holder:  extapi.NewLeaseHolder(ID),
	}
	return svc, versionsDir, probe
}

// seedMDInstalled 造目录形状正确的假资产（文件名按 expectedAssetName 族）。
// PE 身份核验必不过（非真 MangoDisk exe）→ 完整性判 IntegrityInvalid，
// 这正是本文件要钉死的拦截前提，由断言反向自证。
func seedMDInstalled(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "mangodisk_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MangoDisk-"+ver+"-windows-portable.exe"),
		[]byte("MZ fake payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- 下载单飞闸（TryLock 形态的家族差异点） ----------

func TestMangoDiskDownloadSingleFlight(t *testing.T) {
	svc, _, _ := newTestMangoDiskService(t)

	svc.downloadMu.Lock()
	heldByTest := true
	defer func() {
		if heldByTest {
			svc.downloadMu.Unlock()
		}
	}()
	state, err := svc.DownloadVersion("9.9.9")
	if err == nil || !strings.Contains(err.Error(), "正在下载") {
		t.Fatalf("并发下载应被单飞闸拒绝并给可读错误，实得 %q err=%v", state, err)
	}
	if state != "" {
		t.Fatalf("拒绝路径不得谎报受理状态，实得 %q", state)
	}
	// 拒绝路径不得偷锁：原持有者仍可感知锁在手上，且不因被拒调用二次加锁。
	if svc.downloadMu.TryLock() {
		svc.downloadMu.Unlock()
		t.Fatal("前置：测试主协程仍应持有下载锁")
	}

	svc.downloadMu.Unlock()
	heldByTest = false
	if !svc.downloadMu.TryLock() {
		t.Fatal("拒绝早退不得残留锁（解锁后应可再取）")
	}
	svc.downloadMu.Unlock()
}

func TestMangoDiskDownloadAlreadyInstalledSync(t *testing.T) {
	svc, versionsDir, _ := newTestMangoDiskService(t)
	seedMDInstalled(t, versionsDir, "1.0.7")
	for _, ask := range []string{"1.0.7", "v1.0.7"} {
		state, err := svc.DownloadVersion(ask)
		if err != nil || state != "already-installed" {
			t.Fatalf("已装版本入参 %q 应回 already-installed，实得 %q err=%v", ask, state, err)
		}
		if !svc.downloadMu.TryLock() {
			t.Fatal("already-installed 早退不得残留下载锁")
		}
		svc.downloadMu.Unlock()
	}
}

// ---------- 完整性闸门（激活与冷启动两处真实拦截） ----------

func TestMangoDiskSetActiveVersionRejectsInvalidIntegrity(t *testing.T) {
	svc, versionsDir, _ := newTestMangoDiskService(t)
	seedMDInstalled(t, versionsDir, "1.0.7") // 假 PE：身份核验不过 → IntegrityInvalid

	// 前置自证：该资产确实被判无效（否则下面的拦截断言就是空转）。
	info, err := svc.manager.Inspect("v1.0.7")
	if err != nil {
		t.Fatal(err)
	}
	if info.Integrity != version.IntegrityInvalid {
		t.Fatalf("前置：假 PE 应判 IntegrityInvalid，实得 %v（拦截断言失去意义）", info.Integrity)
	}

	if _, err := svc.SetActiveVersion("1.0.7"); err == nil || !strings.Contains(err.Error(), "安装无效") {
		t.Fatalf("完整性无效版本不得可被设为使用，实得 %v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("被拒激活不得落账，实得 %q", got)
	}

	// 冷启动同源拦截：坏 active 指向无效资产时当场清空且回退报错，
	// 绝不把无效载荷交给引擎当启动目标。
	if err := svc.store.SetActive("v1.0.7"); err != nil {
		t.Fatal(err)
	}
	v, exe, rerr := svc.resolveActiveVersion()
	if rerr == nil || !strings.Contains(rerr.Error(), "尚未安装") {
		t.Fatalf("全部已装均无效时冷启动应报错，实得 %q/%q err=%v", v, exe, rerr)
	}
	reloaded := newMangoDiskStore(filepath.Dir(svc.store.filePath))
	if got := reloaded.GetActive(); got != "" {
		t.Fatalf("无效 active 自愈清空须落盘，实得 %q", got)
	}
}

// ---------- external 接管后的导入/退出/唤窗处置 ----------

func TestMangoDiskExternalAdoptionGuards(t *testing.T) {
	svc, _, probe := newTestMangoDiskService(t)
	probe.set(true, 4242)

	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateExternal || snap.PID != 4242 {
		t.Fatalf("互斥体+信号窗应接管为 external（携信号 PID），实得 %+v", snap)
	}

	// 运行中导入拒绝（新旧文件混用防线）。
	if _, ierr := svc.ImportLocal(`C:\nonexistent\MangoDisk.exe`); ierr == nil ||
		!strings.Contains(ierr.Error(), "正在运行") {
		t.Fatalf("external 在场时导入必须被拒，实得 %v", ierr)
	}

	// 外部实例不越权终止。
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External || out.Message == "" {
		t.Fatalf("external 退出应只回指引，实得 %+v", out)
	}

	// 唤窗借用本地 exe 的单实例信使语义：本地无完整性可用件时如实报错，
	// 绝不静默"假装唤起"。
	_, werr := svc.OpenWindow()
	if werr == nil || !strings.Contains(werr.Error(), "尚未安装") {
		t.Fatalf("无可用本地件时 external 唤窗须如实报错，实得 %v", werr)
	}

	// 外部实例消失后复探收敛。
	probe.set(false, 0)
	snap2, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap2.State != instance.StateStopped {
		t.Fatalf("外部退出后应回 stopped，实得 %s", snap2.State)
	}
}

func TestMangoDiskQuitStoppedIdempotent(t *testing.T) {
	svc, _, _ := newTestMangoDiskService(t)
	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || out.External {
		t.Fatalf("静止会话退出应幂等成功，实得 %+v", out)
	}
}

// ---------- 调用门拒接矩阵 ----------

func TestMangoDiskGateRefusesRPCSurface(t *testing.T) {
	svc, _, _ := newTestMangoDiskService(t)
	sentinel := errors.New("模块已停用：调用门拒接")
	svc.holder.SetGate(denyGateMD{sentinel: sentinel})

	checks := []struct {
		name string
		call func() error
	}{
		{"ListReleases", func() error { _, err := svc.ListReleases(); return err }},
		{"ListInstalledVersions", func() error { _, err := svc.ListInstalledVersions(); return err }},
		{"DownloadVersion", func() error { _, err := svc.DownloadVersion("1.0.7"); return err }},
		{"RemoveVersion", func() error { return svc.RemoveVersion("1.0.7") }},
		{"SetActiveVersion", func() error { _, err := svc.SetActiveVersion("1.0.7"); return err }},
		{"GetActiveVersion", func() error { _, err := svc.GetActiveVersion(); return err }},
		{"ImportLocal", func() error { _, err := svc.ImportLocal(`C:\nonexistent.exe`); return err }},
		{"GetStatus", func() error { _, err := svc.GetStatus(); return err }},
		{"OpenWindow", func() error { _, err := svc.OpenWindow(); return err }},
		{"Quit", func() error { _, err := svc.Quit(); return err }},
		{"OpenDir", func() error { return svc.OpenDir(`C:\nonexistent`) }},
		{"RepositoryURL", func() error { _, err := svc.RepositoryURL(); return err }},
		{"OpenRepository", func() error { return svc.OpenRepository() }},
		{"CreateDesktopShortcut", func() error { return svc.CreateDesktopShortcut() }},
		{"GetFollowOnExit", func() error { _, err := svc.GetFollowOnExit(); return err }},
		{"SetFollowOnExit", func() error { return svc.SetFollowOnExit(true) }},
	}
	for _, c := range checks {
		if err := c.call(); !errors.Is(err, sentinel) {
			t.Errorf("%s 门拒应原样上抛调用门错误，实得 %v", c.name, err)
		}
	}
	// 门拒不得残留下载锁、不得动账。
	if !svc.downloadMu.TryLock() {
		t.Error("门拒路径残留了 downloadMu")
	}
	svc.downloadMu.Unlock()
	if got := svc.store.GetActive(); got != "" {
		t.Errorf("门拒期间 active 不得变动，实得 %q", got)
	}
	if svc.store.GetFollowOnExit() {
		t.Error("门拒期间 followOnExit 不得被写 true")
	}
	// Shutdown 为 void 签名：拒即早退，只验证不 panic 且不动偏好账。
	svc.Shutdown()
	if svc.store.GetFollowOnExit() {
		t.Error("Shutdown 门拒早退不得改写偏好")
	}
}
