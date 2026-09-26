package windterm

import (
	"testing"

	"hanxi/internal/modules/windterm/version"
)

// 首装自动设使用的落账必须在 done 成功回执广播之前：前端共享 store 收到 done
// 即复刷版本区读 GetActiveVersion，事件先行会让瞬时复刷读到空值（"首个版本下载
// 完不显示使用中"，与 termora/2ac9b3b 同根修）。本测试经注入广播接缝锁死该时序，
// 不触真下载。

func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &WindTermService{store: newWindtermStore(t.TempDir())}
	var observedAtBroadcast string
	svc.settleAndBroadcastProgress("v2.6.1", version.DownloadProgress{Version: "v2.6.1", Stage: "done"},
		func(version.DownloadProgress) {
			observedAtBroadcast = svc.store.GetActive()
		})
	if observedAtBroadcast != "v2.6.1" {
		t.Fatalf("done 回执广播时刻 active 应已落账，实得 %q", observedAtBroadcast)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &WindTermService{store: newWindtermStore(t.TempDir())}
	if err := svc.store.SetActive("v2.5.0"); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("v2.6.1", version.DownloadProgress{Version: "v2.6.1", Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "v2.5.0" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &WindTermService{store: newWindtermStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("v2.6.1", version.DownloadProgress{Version: "v2.6.1", Stage: "extract"},
		func(version.DownloadProgress) {})
	if got := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}
