package mangodisk

import (
	"testing"

	"hanxi/internal/modules/mangodisk/version"
)

func TestNormalizeVersion(t *testing.T) {
	if got := normalizeVersion("1.0.7"); got != "v1.0.7" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeVersion("v1.0.7"); got != "v1.0.7" {
		t.Fatalf("got %q", got)
	}
}

// 首装自动设使用的落账必须在 done 成功回执广播之前：前端共享 store 收到 done
// 即复刷版本区读 GetActiveVersion，事件先行会让瞬时复刷读到空值（"首个版本下载
// 完不显示使用中"，与 termora/2ac9b3b 同根修）。本测试经注入广播接缝锁死该时序，
// 不触真下载。

func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &MangoDiskService{store: newMangoDiskStore(t.TempDir())}
	var observedAtBroadcast string
	svc.settleAndBroadcastProgress("v1.0.7", version.DownloadProgress{Version: "v1.0.7", Stage: "done"},
		func(version.DownloadProgress) {
			observedAtBroadcast = svc.store.GetActive()
		})
	if observedAtBroadcast != "v1.0.7" {
		t.Fatalf("done 回执广播时刻 active 应已落账，实得 %q", observedAtBroadcast)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &MangoDiskService{store: newMangoDiskStore(t.TempDir())}
	if err := svc.store.SetActive("v1.0.5"); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("v1.0.7", version.DownloadProgress{Version: "v1.0.7", Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "v1.0.5" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &MangoDiskService{store: newMangoDiskStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("v1.0.7", version.DownloadProgress{Version: "v1.0.7", Stage: "verify"},
		func(version.DownloadProgress) {})
	if got := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}
