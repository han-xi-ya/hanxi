package rustdesk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newRustDeskStore(dir)

	if s.GetActive() != "" {
		t.Error("初始 activeVersion 应为空")
	}
	if !s.GetFollowOnExit() {
		t.Error("followOnExit 默认应为 true")
	}

	if err := s.SetActive("v1.4.9"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}

	// 重新加载验证落盘
	s2 := newRustDeskStore(dir)
	if s2.GetActive() != "v1.4.9" {
		t.Errorf("activeVersion = %q, want v1.4.9", s2.GetActive())
	}
	if s2.GetFollowOnExit() {
		t.Error("followOnExit 应持久化为 false")
	}
}

func TestStoreCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rustdesk.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRustDeskStore(dir)
	if s.GetActive() != "" || !s.GetFollowOnExit() {
		t.Errorf("损坏文件应按空配置继续: active=%q follow=%v", s.GetActive(), s.GetFollowOnExit())
	}
}

func TestStoreFollowOnExitBackCompat(t *testing.T) {
	// 老配置无 followOnExit 字段 → 默认 true
	dir := t.TempDir()
	path := filepath.Join(dir, "rustdesk.json")
	if err := os.WriteFile(path, []byte(`{"activeVersion":"v1.4.8"}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRustDeskStore(dir)
	if s.GetActive() != "v1.4.8" || !s.GetFollowOnExit() {
		t.Errorf("回读错误: active=%q follow=%v", s.GetActive(), s.GetFollowOnExit())
	}
}
