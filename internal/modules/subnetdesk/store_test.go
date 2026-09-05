package subnetdesk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newSubnetDeskStore(dir)

	if s.GetActive() != "" {
		t.Error("初始 activeVersion 应为空")
	}
	if !s.GetFollowOnExit() {
		t.Error("followOnExit 默认应为 true")
	}

	if err := s.SetActive("v1.3.0"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}

	// 重新加载验证落盘
	s2 := newSubnetDeskStore(dir)
	if s2.GetActive() != "v1.3.0" {
		t.Errorf("activeVersion = %q, want v1.3.0", s2.GetActive())
	}
	if s2.GetFollowOnExit() {
		t.Error("followOnExit 应持久化为 false")
	}
}

func TestStoreCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subnetdesk.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newSubnetDeskStore(dir)
	if s.GetActive() != "" || !s.GetFollowOnExit() {
		t.Errorf("损坏文件应按空配置继续: active=%q follow=%v", s.GetActive(), s.GetFollowOnExit())
	}
}

func TestStoreFollowOnExitBackCompat(t *testing.T) {
	// 老配置无 followOnExit 字段 → 默认 true
	dir := t.TempDir()
	path := filepath.Join(dir, "subnetdesk.json")
	if err := os.WriteFile(path, []byte(`{"activeVersion":"v1.2.3"}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newSubnetDeskStore(dir)
	if s.GetActive() != "v1.2.3" || !s.GetFollowOnExit() {
		t.Errorf("回读错误: active=%q follow=%v", s.GetActive(), s.GetFollowOnExit())
	}
}
