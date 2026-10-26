package quicklook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/quicklook/version"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 判据取在 done 送达后、DownloadContext 尚未返回的时点（channel 建立同步）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	newSvc := func(t *testing.T) (*QuickLookService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &QuickLookService{
			manager: version.NewManager(t.TempDir()),
			store:   newQuicklookStore(t.TempDir()),
			holder:  extapi.NewLeaseHolder(ID),
		}
		svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
			emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
			emit(version.DownloadProgress{Version: ver, Stage: "extract"})
			emit(version.DownloadProgress{Version: ver, Stage: "done", Done: 100, Total: 100})
			activeAtDone <- svc.store.GetActive()
			return nil
		}
		return svc, activeAtDone
	}
	receive := func(t *testing.T, ch chan string) string {
		t.Helper()
		select {
		case got := <-ch:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("后台下载收口未在时限内送达 done")
			return ""
		}
	}

	t.Run("首装done即落账", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if _, err := svc.DownloadVersion("v0.9.1"); err != nil {
			t.Fatal(err)
		}
		// quicklook 版本口径去 v 前缀
		if got := receive(t, activeAtDone); got != "0.9.1" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 0.9.1，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("0.9.0"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("v0.9.1"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "0.9.0" {
			t.Fatalf("done 送达时 active 应保持用户手选的 0.9.0，实得 %q", got)
		}
	})
}

// newDownloadTestService 装配下载面单测最小 service（translucenttb e65364d
// harness 同法）：调用门挂新 holder（Enter/EnterBackground 直通），版本管理器
// 指向空临时目录（ListInstalled 如实回空），下载经 download 缝注入桩——
// 零真实网络、零真实落位。
func newDownloadTestService(t *testing.T) *QuickLookService {
	t.Helper()
	dir := t.TempDir()
	return &QuickLookService{
		manager:   version.NewManager(filepath.Join(dir, "versions")),
		store:     newQuicklookStore(dir),
		downloads: map[string]struct{}{},
		holder:    extapi.NewLeaseHolder(ID),
	}
}

// TestDownloadVersionInFlightDedupe 钉死"连点不双下"：旧实现 downloadMu 只圈
// 同步段，goroutine 下载全程锁外跑——同版本连点照样开两路下载、两笔 journal
// 事务。现按 translucenttb e65364d/piik 名册先例去重，断言四个外部事实：
//  1. 在飞期间的重复请求（含带 v 前缀的归一化同版本）回 "in-progress"，
//     且下载驱动调用计数恒 1（第二路根本没起）；
//  2. 在飞一路收口后名册自动销账，同版本可再次发起（拒绝路径不粘闸）；
//  3. 名册按版本维度隔离：A 在飞不挡 B 起步；
//  4. 已装版本连点回 already-installed（既有回执词表不回归）。
func TestDownloadVersionInFlightDedupe(t *testing.T) {
	svc := newDownloadTestService(t)
	var calls091 atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	svc.download = func(_ context.Context, _, targetVersion string, _ func(version.DownloadProgress)) error {
		if targetVersion == "0.9.1" && calls091.Add(1) == 1 {
			close(started) // 驱动确已进入（此后读名册才是"在飞"实况）
		}
		<-release // 模拟慢下载在飞
		return nil
	}

	// 首发带 v 前缀：归一化后与裸号共用同一本名册
	if state, err := svc.DownloadVersion("v0.9.1"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动 5s 内未启动")
	}

	for i, ask := range []string{"0.9.1", "v0.9.1"} {
		if state, err := svc.DownloadVersion(ask); err != nil || state != "in-progress" {
			t.Fatalf("在飞期间的重复请求（第 %d 次，请求词 %q）应回 in-progress: state=%q err=%v", i+1, ask, state, err)
		}
	}
	if got := calls091.Load(); got != 1 {
		t.Fatalf("重复点击触发了额外下载路次（驱动调用 %d 次）", got)
	}

	// 名册按版本隔离：0.9.1 在飞不挡 0.9.0 起步
	var calls090 atomic.Int32
	svc.download = func(_ context.Context, _, targetVersion string, _ func(version.DownloadProgress)) error {
		if targetVersion == "0.9.0" {
			calls090.Add(1)
		}
		<-release
		return nil
	}
	if state, err := svc.DownloadVersion("0.9.0"); err != nil || state != "started" {
		t.Fatalf("未占名册的版本应正常受理: state=%q err=%v", state, err)
	}

	// 放落在飞两路：名册须自动销账（defer 含早退路径），同版本可再发起
	close(release)
	waitRosterFree(t, svc, "0.9.1", 10*time.Second)
	waitRosterFree(t, svc, "0.9.0", 10*time.Second)

	var reran atomic.Int32
	svc.download = func(_ context.Context, _, _ string, _ func(version.DownloadProgress)) error {
		reran.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("0.9.1"); err != nil || state != "started" {
		t.Fatalf("收口后同版本应可再次发起: state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("重发起的下载驱动未被调用")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRosterFree(t, svc, "0.9.1", 10*time.Second) // 收尾不留后台在飞协程

	// 已装免下回归：名册空窗后走 ListInstalled 真实账判"已装"（既有回执词不回归）
	installedVer := plantInstalled(t, svc)
	if state, err := svc.DownloadVersion(installedVer); err != nil || state != "already-installed" {
		t.Fatalf("已装版本连点应回 already-installed: state=%q err=%v", state, err)
	}
}

// waitRosterFree 轮询在飞名册直至该版本销账——判据是名册实况（外部事实回读），
// budget 只是挂死兜底而非"放大超时凑绿"；translucenttb e65364d harness 同法。
func waitRosterFree(t *testing.T, svc *QuickLookService, v string, budget time.Duration) {
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

// TestDownloadErrorPathClearsRoster 钉死失败早退不粘闸：下载驱动模拟失败后
// goroutine 经 txn.Fail 收口退出，名册必须自动销账且同版本可再发起；
// 并断下载缝收到的是可取消的事务 ctx（Done() 非 nil——租约/生命周期贯穿的
// 外部事实，若是 context.Background() 脱钩形态则停用/退出永远取消不了下载）。
func TestDownloadErrorPathClearsRoster(t *testing.T) {
	svc := newDownloadTestService(t)
	driveCtx := make(chan context.Context, 1)
	svc.download = func(ctx context.Context, _, _ string, _ func(version.DownloadProgress)) error {
		driveCtx <- ctx
		return errors.New("模拟下载失败")
	}
	if state, err := svc.DownloadVersion("0.9.1"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case ctx := <-driveCtx:
		if ctx.Done() == nil {
			t.Fatal("下载缝应收到可取消的事务 ctx（lease/txn 生命周期贯穿），实收不可取消 context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动 5s 内未启动")
	}

	// 失败早退即收口：名册须销账（defer 在函数头注册，覆盖 Fail 早退路径）
	waitRosterFree(t, svc, "0.9.1", 10*time.Second)

	var reran atomic.Int32
	svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		reran.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("0.9.1"); err != nil || state != "started" {
		t.Fatalf("失败收口后同版本应可再发起: state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("重发起的下载驱动未被调用")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRosterFree(t, svc, "0.9.1", 10*time.Second)
}

// plantInstalled 造一个伪便携目录（QuickLook.exe 假文件）经 ImportLocal 真实
// 落进 manager 隔离树，返回入账版本令牌（假 exe 读不出 FileVersion，走
// imported-<哈希> 兜底形态——与 ListInstalled 取数同一口径，不断字符串形状）。
func plantInstalled(t *testing.T, svc *QuickLookService) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "QuickLook.exe"), []byte("fake-exe"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := svc.manager.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	return info.Version
}
