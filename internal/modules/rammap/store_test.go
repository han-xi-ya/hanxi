package rammap

import (
	"os"
	"path/filepath"
	"testing"
)

// T2 落盘账本：activeVersion/followOnExit 必须"当场写盘"，重启（新 store
// 读同一目录）后取值与断电前一致；损坏文件容忍不 panic、不吞后续写入。

func TestRammapStorePersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s := newRammapStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("新目录应双零值（未指定+默认不联动），实得 %q/%v", s.GetActive(), s.GetFollowOnExit())
	}

	if err := s.SetActive("2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	// 落盘为同步语义：SetActive 返回即应可被"重启"读到（无需等异步刷盘）。
	reloaded := newRammapStore(dir)
	if got := reloaded.GetActive(); got != "2026-09-01" {
		t.Fatalf("重启后 active 应保持，实得 %q", got)
	}
	if !reloaded.GetFollowOnExit() {
		t.Fatal("重启后 followOnExit 应保持 true")
	}

	// 清空也要持久：SetActive("") 不得被旧盘值复活。
	if err := s.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if got := newRammapStore(dir).GetActive(); got != "" {
		t.Fatalf("清空后重启应仍为空，实得 %q", got)
	}
	// 清 active 不得连带改 followOnExit（两字段独立账）。
	if !newRammapStore(dir).GetFollowOnExit() {
		t.Fatal("SetActive(\"\") 不得重置 followOnExit")
	}
}

func TestRammapStoreCorruptFileTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rammap.json")
	if err := os.WriteFile(path, []byte("{损坏不是JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newRammapStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏账本应按空配置继续，实得 %q/%v", s.GetActive(), s.GetFollowOnExit())
	}
	// 损坏后仍可正常写新账并重启读取。
	if err := s.SetActive("2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if got := newRammapStore(dir).GetActive(); got != "2026-09-01" {
		t.Fatalf("损坏账本修复后应可回读，实得 %q", got)
	}
}
