package snipaste

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/snipaste/version"
)

func TestSnipasteStoreRoundTripAndDamageTolerance(t *testing.T) {
	dir := t.TempDir()
	s := newSnipasteStore(dir)
	if err := s.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	if got := newSnipasteStore(dir).GetActive(); got != "2.11.3" {
		t.Fatalf("active=%q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "snipaste.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := newSnipasteStore(dir).GetActive(); got != "" {
		t.Fatalf("damaged active=%q", got)
	}
}

func TestResolveActiveVersion(t *testing.T) {
	versionsDir := t.TempDir()
	dataDir := t.TempDir()
	dir := filepath.Join(versionsDir, "snipaste_v2.11.3")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "Snipaste.exe")
	if err := os.WriteFile(exe, []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := version.NewManager(versionsDir)
	store := newSnipasteStore(dataDir)
	if err := store.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	svc := &SnipasteService{manager: manager, store: store, downloads: map[string]struct{}{}}
	selected, gotExe, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if selected != "2.11.3" || gotExe != exe {
		t.Fatalf("selected=%q exe=%q", selected, gotExe)
	}
}

func TestRemoveActiveVersionRejected(t *testing.T) {
	store := newSnipasteStore(t.TempDir())
	if err := store.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	svc := &SnipasteService{manager: version.NewManager(t.TempDir()), store: store, downloads: map[string]struct{}{}, holder: extapi.NewLeaseHolder(ID)}
	if err := svc.RemoveVersion("2.11.3"); err == nil {
		t.Fatal("active version removal should fail")
	}
}

// 首装自动设使用的落账必须在 done 成功回执广播之前：前端票据流程收到 done 即
// 复刷本地版本读 GetActiveVersion，事件先行会让瞬时复刷读到空值（与
// termora/2ac9b3b 同根修）。本测试经注入广播接缝锁死该时序，不触真下载。
func TestDoneReceiptSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := &SnipasteService{store: newSnipasteStore(t.TempDir())}
	var observedAtBroadcast string
	svc.settleAndBroadcastProgress("2.11.2", version.DownloadProgress{Version: "2.11.2", Stage: "done"},
		func(version.DownloadProgress) {
			observedAtBroadcast = svc.store.GetActive()
		})
	if observedAtBroadcast != "2.11.2" {
		t.Fatalf("done 回执广播时刻 active 应已落账，实得 %q", observedAtBroadcast)
	}
}

// 回归护栏：已设定使用时后续 done 不抢账；非 done 阶段不落账。
func TestSettleOnlyOnFirstDone(t *testing.T) {
	svc := &SnipasteService{store: newSnipasteStore(t.TempDir())}
	if err := svc.store.SetActive("2.10.8"); err != nil {
		t.Fatal(err)
	}
	svc.settleAndBroadcastProgress("2.11.2", version.DownloadProgress{Version: "2.11.2", Stage: "done"},
		func(version.DownloadProgress) {})
	if got := svc.store.GetActive(); got != "2.10.8" {
		t.Fatalf("已设定使用时 done 不应改写 active，实得 %q", got)
	}
	svc2 := &SnipasteService{store: newSnipasteStore(t.TempDir())}
	svc2.settleAndBroadcastProgress("2.11.2", version.DownloadProgress{Version: "2.11.2", Stage: "install"},
		func(version.DownloadProgress) {})
	if got := svc2.store.GetActive(); got != "" {
		t.Fatalf("非 done 阶段不应落账，实得 %q", got)
	}
}
