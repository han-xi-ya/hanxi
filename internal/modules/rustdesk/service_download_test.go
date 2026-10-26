// service_download_test.go 锁死 rustdesk 下载面的两组时序事实（X1 修复批）：
//
//   - D3（首装 done 先行于 active 落账）：首装自动设使用的落账必须发生在 done
//     事件广播之前——前端共享 store 收到 done 即复刷版本区读 GetActiveVersion，
//     事件先行会让瞬时复刷读到空值（termora 2ac9b3b / windterm/rammap 同根修）。
//   - D5（同版本连点双路下载）：downloadMu 旧形态只圈同步段，goroutine 全程锁外
//     跑，同版本连点照样两路并发下载、两路 rename 同一目标、双 done 双通知；
//     现按 quicklook ceceabd / translucenttb e65364d 名册先例去重。
//
// 断言对象是外部事实：广播回调里读到的 store 实况、下载驱动调用计数、名册
// 销账后可再发起。时序一律 channel 事件驱动，零真实网络。
package rustdesk

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/modules/rustdesk/version"
)

// ---------- D3：done 回执先落账、后广播 ----------

func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &RustDeskService{store: newRustDeskStore(t.TempDir())}
	var seenVer, seenForm string
	svc.settleAndBroadcastProgress("v1.2.6", version.DownloadProgress{Version: "v1.2.6", Stage: "done"},
		func(version.DownloadProgress) {
			seenVer, seenForm = svc.store.GetActive()
		})
	if seenVer != "v1.2.6" || seenForm != version.FormPortable {
		t.Fatalf("done 回执广播时刻 active 应已落账为 v1.2.6/portable，实得 %q/%q", seenVer, seenForm)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &RustDeskService{store: newRustDeskStore(t.TempDir())}
	if err := svc.store.SetActive("v1.2.0", version.FormPortable); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("v1.2.6", version.DownloadProgress{Version: "v1.2.6", Stage: "done"},
		func(version.DownloadProgress) {})
	if got, _ := svc.store.GetActive(); got != "v1.2.0" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &RustDeskService{store: newRustDeskStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("v1.2.6", version.DownloadProgress{Version: "v1.2.6", Stage: "install"},
		func(version.DownloadProgress) {})
	if got, _ := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}

// TestDownloadDoneSettlesBeforeDriverReturn 端到端时序锁（D3 命门）：经下载驱动
// 缝注入假下载体，在 done 送达后、drive 尚未返回的时点回读 store——旧实现把落账
// 放在 DownloadContext 返回之后，此刻读到空值必红。判据取真实 goroutine 的
// 账本快照，channel 建立同步，不触网络。
func TestDownloadDoneSettlesBeforeDriverReturn(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	settled := make(chan string, 1)
	svc.downloadDriver = func(_ context.Context, v string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: v, Stage: "downloading", Done: 10, Total: 100})
		emit(version.DownloadProgress{Version: v, Stage: "done", Done: 100, Total: 100})
		got, _ := svc.store.GetActive()
		settled <- got
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.3"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case got := <-settled:
		if got != "v99.0.3" {
			t.Fatalf("done 送达时（下载体未返回）active 应已落账 v99.0.3，实得 %q——事件先行于落账即原缺陷", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("后台下载收口未在时限内送达 done")
	}
	waitRosterFree(t, svc, "v99.0.3", 10*time.Second)
}

// ---------- D5：在飞名册去重 ----------

// TestDownloadVersionInFlightDedupe 钉死"连点不双下"，断四个外部事实：
//  1. 在飞期间的重复请求（含不带 v 前缀的归一化同版本）回 "in-progress"，
//     且下载驱动调用计数恒 1（第二路根本没起）；
//  2. 在飞一路收口后名册自动销账，同版本可再次发起（拒绝路径不粘闸）；
//  3. 名册按版本维度隔离：A 在飞不挡 B 起步；
//  4. 已装版本连点回 already-installed（既有回执词表不回归）。
func TestDownloadVersionInFlightDedupe(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	var calls991 atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	svc.downloadDriver = func(_ context.Context, targetVersion string, _ func(version.DownloadProgress)) error {
		if targetVersion == "v99.0.1" && calls991.Add(1) == 1 {
			close(started) // 驱动确已进入（此后读名册才是"在飞"实况）
		}
		<-release // 模拟慢下载在飞
		return nil
	}

	if state, err := svc.DownloadVersion("v99.0.1"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动 5s 内未启动")
	}

	for i, ask := range []string{"99.0.1", "v99.0.1"} {
		if state, err := svc.DownloadVersion(ask); err != nil || state != "in-progress" {
			t.Fatalf("在飞期间的重复请求（第 %d 次，请求词 %q）应回 in-progress: state=%q err=%v", i+1, ask, state, err)
		}
	}
	if got := calls991.Load(); got != 1 {
		t.Fatalf("重复点击触发了额外下载路次（驱动调用 %d 次）", got)
	}

	// 名册按版本隔离：99.0.1 在飞不挡 99.0.2 起步
	var calls992 atomic.Int32
	svc.downloadDriver = func(_ context.Context, targetVersion string, _ func(version.DownloadProgress)) error {
		if targetVersion == "v99.0.2" {
			calls992.Add(1)
		}
		<-release
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.2"); err != nil || state != "started" {
		t.Fatalf("未占名册的版本应正常受理: state=%q err=%v", state, err)
	}

	// 放落在飞两路：名册须自动销账（defer 含早退路径），同版本可再发起
	close(release)
	waitRosterFree(t, svc, "v99.0.1", 10*time.Second)
	waitRosterFree(t, svc, "v99.0.2", 10*time.Second)

	var reran atomic.Int32
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		reran.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.1"); err != nil || state != "started" {
		t.Fatalf("收口后同版本应可再次发起: state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("重发起的下载驱动未被调用")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRosterFree(t, svc, "v99.0.1", 10*time.Second) // 收尾不留后台在飞协程

	// 已装免下回归：走 ListInstalled 真实账判"已装"（既有回执词不回归）
	seedRDInstalled(t, versionsDir, "99.0.9")
	if state, err := svc.DownloadVersion("v99.0.9"); err != nil || state != "already-installed" {
		t.Fatalf("已装版本连点应回 already-installed: state=%q err=%v", state, err)
	}
}

// TestDownloadErrorPathClearsRoster 钉死失败早退不粘闸：驱动模拟失败后
// goroutine 退出，名册必须自动销账且同版本可再发起；并断下载缝收到的是
// 可取消的 ctx（Done() 非 nil——租约贯穿的外部事实，若是 context.Background()
// 脱钩形态则停用/退出永远取消不了下载）。
func TestDownloadErrorPathClearsRoster(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	driveCtx := make(chan context.Context, 1)
	svc.downloadDriver = func(ctx context.Context, _ string, _ func(version.DownloadProgress)) error {
		driveCtx <- ctx
		return errors.New("模拟下载失败")
	}
	if state, err := svc.DownloadVersion("v99.0.5"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case ctx := <-driveCtx:
		if ctx.Done() == nil {
			t.Fatal("下载缝应收到可取消的租约 ctx（lease.Context() 贯穿），实收不可取消 context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动未被调用")
	}
	waitRosterFree(t, svc, "v99.0.5", 10*time.Second)

	var reran atomic.Int32
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		reran.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.5"); err != nil || state != "started" {
		t.Fatalf("失败收口后同版本应可再次发起: state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("重发起的下载驱动未被调用")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRosterFree(t, svc, "v99.0.5", 10*time.Second)
}

// TestInstallerChannelHasOwnRoster 断安装版通道与便携通道按"通道+版本"分键：
// 便携 v99.0.4 在飞不挡安装版同版本起步；安装版自身连点回 in-progress 且
// MSI 下载驱动调用计数恒 1。
func TestInstallerChannelHasOwnRoster(t *testing.T) {
	svc, _, _ := newTestRustDeskService(t)
	// 便携一路先占名册（驱动阻塞）
	portableStarted := make(chan struct{})
	release := make(chan struct{})
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		close(portableStarted)
		<-release
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.4"); err != nil || state != "started" {
		t.Fatalf("便携下载应受理: state=%q err=%v", state, err)
	}
	select {
	case <-portableStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("便携下载驱动 5s 内未启动")
	}

	var installerCalls atomic.Int32
	installerStarted := make(chan struct{})
	svc.installerDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		if installerCalls.Add(1) == 1 {
			close(installerStarted)
		}
		<-release
		return errors.New("收口桩：放行即失败退出，绝不走进 manager.Install")
	}
	if state, err := svc.InstallVersion("v99.0.4"); err != nil || state != "started" {
		t.Fatalf("安装版通道独立键，便携在飞不应挡装机: state=%q err=%v", state, err)
	}
	select {
	case <-installerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("安装版 MSI 下载驱动 5s 内未启动")
	}
	if state, err := svc.InstallVersion("99.0.4"); err != nil || state != "in-progress" {
		t.Fatalf("安装版同版本连点应回 in-progress: state=%q err=%v", state, err)
	}
	close(release)
	waitRosterFree(t, svc, "v99.0.4", 10*time.Second)
	waitRosterFree(t, svc, installerRosterPrefix+"v99.0.4", 10*time.Second)
	if got := installerCalls.Load(); got != 1 {
		t.Fatalf("安装版连点触发了额外 MSI 下载路次（驱动调用 %d 次）", got)
	}
}

// waitRosterFree 轮询在飞名册直至该键销账——判据是名册实况（外部事实回读），
// budget 只是挂死兜底而非"放大超时凑绿"；translucenttb e65364d harness 同法。
func waitRosterFree(t *testing.T, svc *RustDeskService, key string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		svc.downloadMu.Lock()
		_, busy := svc.downloads[key]
		svc.downloadMu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 在飞名册 %s 未销账（残账粘闸）", key, budget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
