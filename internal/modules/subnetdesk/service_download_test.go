// service_download_test.go 锁死 subnetdesk 下载面的三组事实（X1 修复批）：
//
//   - D3（首装 done 先行于 active 落账）：落账必须先于 done 事件广播
//     （termora 2ac9b3b / windterm/rammap 同根修）；
//   - D4（后台下载/安装不入 drain 账、manager 无 ctx）：后台 goroutine 必须
//     经 holder.EnterBackground 取租约贯穿全程，门拒即不启动并如实回执，
//     lease.Context() 送达下载体（rustdesk 同族样板，ADR-0001 §1.4）；
//   - D5（同版本连点双路下载）：按 quicklook ceceabd / translucenttb e65364d
//     名册先例去重，在飞回 "in-progress"、收口 defer 销账。
//
// 断言对象全部是外部事实：广播回调里读到的 store 实况、计数门观察到的
// Acquire/Release/在途数、下载驱动调用计数与名册销账。时序 channel 事件驱动，
// 零真实网络。样板：nanazip/background_lease_test.go。
package subnetdesk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/modules/subnetdesk/version"
)

// ---------- 计数门替身（nanazip countingGate 同型） ----------

type sdCountingGate struct {
	mu       sync.Mutex
	acquires int
	releases int
	inFlight int
}

func (g *sdCountingGate) Acquire(string) (func(), error) {
	var once sync.Once
	g.mu.Lock()
	g.acquires++
	g.inFlight++
	g.mu.Unlock()
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.inFlight--
			g.releases++
			g.mu.Unlock()
		})
	}, nil
}

func (g *sdCountingGate) snapshot() (acquires, releases, inFlight int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.acquires, g.releases, g.inFlight
}

// sdDenySecondGate 只放行第一笔租约的门：RPC 同步段 Enter 取到即放，
// 后台协程 EnterBackground 再取即拒——专测"门拒即不启动、如实回执"。
type sdDenySecondGate struct {
	mu    sync.Mutex
	seens int
	deny  error
}

func (g *sdDenySecondGate) Acquire(string) (func(), error) {
	g.mu.Lock()
	g.seens++
	first := g.seens == 1
	g.mu.Unlock()
	if first {
		return func() {}, nil
	}
	return nil, g.deny
}

// ---------- D3：done 回执先落账、后广播 ----------

func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &SubnetDeskService{store: newSubnetDeskStore(t.TempDir())}
	var seenVer, seenForm string
	svc.settleAndBroadcastProgress("v1.3.0", version.DownloadProgress{Version: "v1.3.0", Stage: "done"},
		func(version.DownloadProgress) {
			seenVer, seenForm = svc.store.GetActive()
		})
	if seenVer != "v1.3.0" || seenForm != version.FormPortable {
		t.Fatalf("done 回执广播时刻 active 应已落账为 v1.3.0/portable，实得 %q/%q", seenVer, seenForm)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &SubnetDeskService{store: newSubnetDeskStore(t.TempDir())}
	if err := svc.store.SetActive("v1.2.0", version.FormPortable); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("v1.3.0", version.DownloadProgress{Version: "v1.3.0", Stage: "done"},
		func(version.DownloadProgress) {})
	if got, _ := svc.store.GetActive(); got != "v1.2.0" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &SubnetDeskService{store: newSubnetDeskStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("v1.3.0", version.DownloadProgress{Version: "v1.3.0", Stage: "verify"},
		func(version.DownloadProgress) {})
	if got, _ := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}

// TestDownloadDoneSettlesBeforeDriverReturn 端到端时序锁（D3 命门）：done 送达后、
// 下载体尚未返回的时点回读 store，账上必须已有值（旧实现落账在 Download 返回
// 之后，此刻读到空值必红）。
func TestDownloadDoneSettlesBeforeDriverReturn(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
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

// ---------- D4：后台下载/安装全程计入 drain 门 ----------

// TestDownloadVersionHeldInDrainGate 后台下载在途期间，计数门必须数到一条
// 在途租约；收口后成对归零。**改造前这条必红**（旧 goroutine 不取租约 →
// 在途恒 0，只有 RPC 段那一笔且早已 release）。
func TestDownloadVersionHeldInDrainGate(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
	gate := &sdCountingGate{}
	svc.holder.SetGate(gate)

	entered := make(chan struct{})
	release := make(chan struct{})
	driveCtx := make(chan context.Context, 1)
	var once sync.Once
	svc.downloadDriver = func(ctx context.Context, _ string, _ func(version.DownloadProgress)) error {
		once.Do(func() { close(entered) })
		driveCtx <- ctx
		<-release
		return nil
	}

	if state, err := svc.DownloadVersion("v99.1.1"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	// RPC 已返回（其 Enter 租约已 release），再等下载体真正进入——此刻的
	// 在途数才是后台租约的实况（channel 事件驱动，不靠睡眠赌时序）。
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("后台下载 5s 内未进入驱动")
	}
	acquires, releases, inFlight := gate.snapshot()
	if inFlight != 1 || acquires != 2 || releases != 1 {
		t.Fatalf("后台在途期间门记账异常：acquires=%d releases=%d inFlight=%d，期望 2/1/1"+
			"（RPC 一笔已放 + 后台一笔在途）——这正是『下载全程不计入 drain 门』的原缺陷", acquires, releases, inFlight)
	}
	// 下载体收到的是租约 ctx（可取消）：模块停用能真正掐断在途下载
	select {
	case ctx := <-driveCtx:
		if ctx.Done() == nil {
			t.Fatal("下载缝应收到可取消的 lease ctx，实收不可取消 context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动未上报 ctx")
	}

	close(release)
	waitRosterFree(t, svc, "v99.1.1", 10*time.Second)
	acquires, releases, inFlight = gate.snapshot()
	if inFlight != 0 || acquires != 2 || releases != 2 {
		t.Fatalf("收口后租约未归零：acquires=%d releases=%d inFlight=%d，期望 2/2/0", acquires, releases, inFlight)
	}
}

// TestInstallVersionHeldInDrainGate 后台安装（MSI 下载→向导→核验）整段同样
// 必须记账；桩驱动以失败退出，绝不走进 manager.Install（不弹真向导）。
func TestInstallVersionHeldInDrainGate(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
	gate := &sdCountingGate{}
	svc.holder.SetGate(gate)

	entered := make(chan struct{})
	release := make(chan struct{})
	driveCtx := make(chan context.Context, 1)
	svc.installerDriver = func(ctx context.Context, _ string, _ func(version.DownloadProgress)) error {
		close(entered)
		driveCtx <- ctx
		<-release
		return errors.New("收口桩：放行即失败退出，绝不走进 manager.Install")
	}

	if state, err := svc.InstallVersion("v99.1.2"); err != nil || state != "started" {
		t.Fatalf("InstallVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("后台安装 5s 内未进入 MSI 下载驱动")
	}
	if _, _, inFlight := gate.snapshot(); inFlight != 1 {
		t.Fatalf("装机流程在途期间门内应恰有 1 条租约，实得 %d", inFlight)
	}
	select {
	case ctx := <-driveCtx:
		if ctx.Done() == nil {
			t.Fatal("MSI 下载缝应收到可取消的 lease ctx，实收不可取消 context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MSI 下载驱动未上报 ctx")
	}

	close(release)
	waitRosterFree(t, svc, installerRosterPrefix+"v99.1.2", 10*time.Second)
	if acquires, releases, inFlight := gate.snapshot(); inFlight != 0 || releases != acquires {
		t.Fatalf("装机流程收口后租约不成对：acquires=%d releases=%d inFlight=%d", acquires, releases, inFlight)
	}
}

// TestDownloadLeaseDeniedDoesNotStartDriver 门拒后台租约时：下载体一次都不许
// 被调用（不启动），error 回执如实走 emit 通道，名册不粘闸、active 不落账。
func TestDownloadLeaseDeniedDoesNotStartDriver(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
	sentinel := errors.New("模块已停用")
	svc.holder.SetGate(&sdDenySecondGate{deny: sentinel})

	var calls atomic.Int32
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("v99.1.3"); err != nil || state != "started" {
		t.Fatalf("RPC 同步段门放行，应受理: state=%q err=%v", state, err)
	}
	// 后台协程取租约被拒即早退：等名册销账作为"协程已退出"的事件判据
	waitRosterFree(t, svc, "v99.1.3", 10*time.Second)
	if got := calls.Load(); got != 0 {
		t.Fatalf("门拒后下载体被调用 %d 次——『门拒即不启动』失守", got)
	}
	if v, _ := svc.store.GetActive(); v != "" {
		t.Fatalf("被拒下载不得落账，实得 %q", v)
	}
}

// ---------- D5：在飞名册去重 ----------

// TestDownloadVersionInFlightDedupe 钉死"连点不双下"，断外部事实：在飞期间
// 重复请求回 "in-progress" 且驱动调用计数恒 1；收口销账后可再发起；名册按
// 版本隔离；已装版本连点回 already-installed（既有回执词表不回归）。
func TestDownloadVersionInFlightDedupe(t *testing.T) {
	svc, versionsDir, _ := newTestSubnetDeskService(t)
	var calls991 atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	svc.downloadDriver = func(_ context.Context, targetVersion string, _ func(version.DownloadProgress)) error {
		if targetVersion == "v99.0.1" && calls991.Add(1) == 1 {
			close(started)
		}
		<-release
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
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		<-release
		return nil
	}
	if state, err := svc.DownloadVersion("v99.0.2"); err != nil || state != "started" {
		t.Fatalf("未占名册的版本应正常受理: state=%q err=%v", state, err)
	}

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
	waitRosterFree(t, svc, "v99.0.1", 10*time.Second)

	// 已装免下回归
	seedSDInstalled(t, versionsDir, "99.0.9")
	if state, err := svc.DownloadVersion("v99.0.9"); err != nil || state != "already-installed" {
		t.Fatalf("已装版本连点应回 already-installed: state=%q err=%v", state, err)
	}
}

// TestDownloadErrorPathClearsRoster 钉死失败早退不粘闸：驱动失败后名册自动
// 销账、同版本可再发起。
func TestDownloadErrorPathClearsRoster(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
	svc.downloadDriver = func(_ context.Context, _ string, _ func(version.DownloadProgress)) error {
		return errors.New("模拟下载失败")
	}
	if state, err := svc.DownloadVersion("v99.0.5"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
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
// 便携在飞不挡装机起步；装机自身连点回 in-progress 且 MSI 驱动计数恒 1。
func TestInstallerChannelHasOwnRoster(t *testing.T) {
	svc, _, _ := newTestSubnetDeskService(t)
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
func waitRosterFree(t *testing.T, svc *SubnetDeskService, key string, budget time.Duration) {
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
