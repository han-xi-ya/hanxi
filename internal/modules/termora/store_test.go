package termora

import (
	"os"
	"path/filepath"
	"testing"
)

// T2 落盘账本：activeVersion/followOnExit 同步写盘、重启回读一致、损坏容忍。

func TestTermoraStorePersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s := newTermoraStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("新目录应双零值，实得 %q/%v", s.GetActive(), s.GetFollowOnExit())
	}

	if err := s.SetActive("v2.17.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	reloaded := newTermoraStore(dir)
	if got := reloaded.GetActive(); got != "v2.17.0" {
		t.Fatalf("重启后 active 应保持，实得 %q", got)
	}
	if !reloaded.GetFollowOnExit() {
		t.Fatal("重启后 followOnExit 应保持 true")
	}

	if err := s.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if got := newTermoraStore(dir).GetActive(); got != "" {
		t.Fatalf("清空应持久化（不得被旧盘值复活），实得 %q", got)
	}
	if !newTermoraStore(dir).GetFollowOnExit() {
		t.Fatal("SetActive(\"\") 不得重置 followOnExit")
	}
}

func TestTermoraStoreCorruptFileTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termora.json")
	if err := os.WriteFile(path, []byte("{损坏不是JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTermoraStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏账本应按空配置继续，实得 %q/%v", s.GetActive(), s.GetFollowOnExit())
	}
	if err := s.SetActive("v2.17.0"); err != nil {
		t.Fatal(err)
	}
	if got := newTermoraStore(dir).GetActive(); got != "v2.17.0" {
		t.Fatalf("损坏后重写应可回读，实得 %q", got)
	}
}
