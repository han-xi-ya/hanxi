package translucenttb

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/translucenttb/version"
)

// newTBDownloadTestService 装配下载面单测最小 service（piik harness 同法）：
// 调用门挂新 holder（Enter/EnterBackground 直通），版本管理器指向空临时目录
// （ListInstalled 如实回空），下载经 downloadDriver 桩——零真实网络、零真实落位。
func newTBDownloadTestService(t *testing.T) *TranslucentTBService {
	t.Helper()
	dir := t.TempDir()
	return &TranslucentTBService{
		manager:   version.NewManager(filepath.Join(dir, "versions")),
		store:     newTranslucentTBStore(dir),
		downloads: map[string]struct{}{},
		holder:    extapi.NewLeaseHolder(ID),
	}
}

// TestDownloadVersionInFlightDedupe 钉死"连点不双下"：旧实现 downloadMu 只圈
// 同步段，goroutine 下载全程锁外跑——同版本连点照样开两路下载、两笔 journal
// 事务。现按 piik 名册先例去重，断言三个外部事实：
//  1. 在飞期间的重复请求回 "in-progress" 且下载驱动调用计数恒 1（第二路根本没起）；
//  2. 在飞一路收口后名册销账，同版本可再次发起（拒绝路径不粘闸）；
//  3. 名册按版本维度隔离：A 在飞不挡 B 起步。
func TestDownloadVersionInFlightDedupe(t *testing.T) {
	svc := newTBDownloadTestService(t)
	var calls2026_6 atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		if targetVersion == "2026.6" && calls2026_6.Add(1) == 1 {
			close(started)
		}
		<-release // 模拟慢下载在飞
		return nil
	}

	if state, err := svc.DownloadVersion("2026.6"); err != nil || state != "started" {
		t.Fatalf("首 DownloadVersion 应受理: state=%q err=%v", state, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动 5s 内未启动")
	}

	for i := 0; i < 2; i++ {
		if state, err := svc.DownloadVersion("2026.6"); err != nil || state != "in-progress" {
			t.Fatalf("在飞期间的重复请求应回 in-progress: state=%q err=%v", state, err)
		}
	}
	if got := calls2026_6.Load(); got != 1 {
		t.Fatalf("重复点击触发了额外下载路次（驱动调用 %d 次）", got)
	}

	// 名册按版本隔离：另一版本不受 2026.6 在飞影响
	var calls2025 atomic.Int32
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		if targetVersion == "2025.12" {
			calls2025.Add(1)
		}
		<-release
		return nil
	}
	if state, err := svc.DownloadVersion("2025.12"); err != nil || state != "started" {
		t.Fatalf("未占名册的版本应正常受理: state=%q err=%v", state, err)
	}

	// 放落在飞两轮，名册须自动销账、同版本可再发起
	close(release)
	waitFree := func(v string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			svc.downloadMu.Lock()
			_, busy := svc.downloads[v]
			svc.downloadMu.Unlock()
			if !busy {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s 在飞名册 10s 未销账", v)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFree("2026.6")
	waitFree("2025.12")

	var reran atomic.Int32
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		reran.Add(1)
		return nil
	}
	if state, err := svc.DownloadVersion("2026.6"); err != nil || state != "started" {
		t.Fatalf("收口后同版本应可再次发起: state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reran.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("重发起的下载驱动未被调用")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitFree("2026.6") // 收尾不留后台在飞协程
}

// 首装自动设使用的落账必须在 done 成功回执广播之前：前端共享 store 收到 done
// 即复刷版本区读 GetActiveVersion，事件先行会让瞬时复刷读到空值（"首个版本下载
// 完不显示使用中"，与 termora/2ac9b3b 同根修）。本测试经注入广播接缝锁死该时序，
// 不触真下载。

func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &TranslucentTBService{store: newTranslucentTBStore(t.TempDir())}
	var observedAtBroadcast string
	svc.settleAndBroadcastProgress("2026.6", version.DownloadProgress{Version: "2026.6", Stage: "done"},
		func(version.DownloadProgress) {
			observedAtBroadcast = svc.store.GetActive()
		})
	if observedAtBroadcast != "2026.6" {
		t.Fatalf("done 回执广播时刻 active 应已落账，实得 %q", observedAtBroadcast)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &TranslucentTBService{store: newTranslucentTBStore(t.TempDir())}
	if err := svc.store.SetActive("2025.12"); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("2026.6", version.DownloadProgress{Version: "2026.6", Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "2025.12" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &TranslucentTBService{store: newTranslucentTBStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("2026.6", version.DownloadProgress{Version: "2026.6", Stage: "extract"},
		func(version.DownloadProgress) {})
	if got := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}
