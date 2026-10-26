package mangodisk

import (
	"testing"

	"hanxi/internal/modules/mangodisk/version"
)

// TestNormalizeVersion 版本入参归一词表：裸号/v号/V号/带空白同落一个 v 规范形；
// imported-/vimported- 兜底令牌恒透传为 vimported-（绝不产出 vvimported-）。
// 大写 V 形态是 67f3bd3 批次的病灶本体——旧表只钉裸号与 v 号，故从未暴露。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.0.7", "v1.0.7"},
		{"v1.0.7", "v1.0.7"},
		{"V1.0.7", "v1.0.7"},
		{"  v1.0.7 ", "v1.0.7"},
		{" V1.0.7\t", "v1.0.7"},
		{"imported-0123456789ab", "vimported-0123456789ab"},
		{"vimported-0123456789ab", "vimported-0123456789ab"},
		{"Vimported-0123456789ab", "vimported-0123456789ab"},
		{"", "v"}, // 空串旧口径即补成 "v"（下游按非法形状拒），本修复不改此行为
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
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
