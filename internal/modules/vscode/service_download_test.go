package vscode

import (
	"testing"

	"hanxi/internal/modules/vscode/version"
)

// 便携版首装自动设使用的落账必须在 done 成功回执广播之前：前端 onProgress 收到
// done 即复刷版本区读 GetActiveVersion，事件先行会让瞬时复刷读到空值（与
// termora/2ac9b3b 同根修）。本测试经注入广播接缝锁死该时序，不触真下载。

func TestPortableDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &VSCodeService{store: newVSCodeStore(t.TempDir())}
	var observedAtBroadcast string
	svc.settleAndBroadcastProgress("v1.106.0", version.DownloadProgress{Version: "1.106.0", Form: string(version.FormPortable), Stage: "done"},
		func(version.DownloadProgress) {
			observedAtBroadcast = svc.store.GetActive()
		})
	if observedAtBroadcast != "v1.106.0" {
		t.Fatalf("便携版 done 回执广播时刻 active 应已落账，实得 %q", observedAtBroadcast)
	}
}

// 形态闸与账目护栏：安装版 done 不落账（无"设定使用版本"概念）；已设定使用时
// 便携版 done 不抢账；非 done 阶段不落账。
func TestSettleGatesFormAndFirstDone(t *testing.T) {
	svc := &VSCodeService{store: newVSCodeStore(t.TempDir())}
	svc.settleAndBroadcastProgress("v1.106.0", version.DownloadProgress{Version: "1.106.0", Form: string(version.FormInstaller), Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("安装版 done 不应落账，实得 %q", got)
	}
	if err := svc.store.SetActive("v1.105.0"); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("v1.106.0", version.DownloadProgress{Version: "1.106.0", Form: string(version.FormPortable), Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "v1.105.0" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &VSCodeService{store: newVSCodeStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("v1.106.0", version.DownloadProgress{Version: "1.106.0", Form: string(version.FormPortable), Stage: "downloading"},
		func(version.DownloadProgress) {})
	if got := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}
