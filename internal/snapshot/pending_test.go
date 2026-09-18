package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagePendingRestoreRejectsMemoAndTraversal(t *testing.T) {
	dataDir := t.TempDir()
	for _, rel := range []string{"memo/a.md", "../config.json", "runtime/x.toml"} {
		if err := StagePendingRestore(dataDir, rel, []byte("x")); err == nil {
			t.Errorf("StagePendingRestore(%q) 应拒绝", rel)
		}
	}
}

func TestApplyPendingRestoresRejectsTamperedPayload(t *testing.T) {
	dataDir := t.TempDir()
	if err := StagePendingRestore(dataDir, "state/a.json", []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, snapshotsDirName, pendingRestoreDirName)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	payload := filepath.Join(root, entries[0].Name(), pendingPayloadName)
	if err := os.WriteFile(payload, []byte(`{"v":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPendingRestores(dataDir); err == nil {
		t.Fatal("篡改 payload 应被校验拒绝")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state", "a.json")); !os.IsNotExist(err) {
		t.Fatalf("坏包不得写入目标: %v", err)
	}
}

func TestApplyPendingRestoresRejectsLinkTarget(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	if err := StagePendingRestore(dataDir, "state/a.json", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	makeDirLink(t, outside, filepath.Join(dataDir, rootState))
	if _, err := ApplyPendingRestores(dataDir); err == nil {
		t.Fatal("恢复目标链接越界应拒绝")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.json")); !os.IsNotExist(err) {
		t.Fatalf("不得写到数据根外: %v", err)
	}
}
