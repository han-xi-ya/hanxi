package rustdesk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newRustDeskStore(dir)

	if v, f := s.GetActive(); v != "" || f != "" {
		t.Error("初始 activeVersion/activeForm 应为空")
	}
	if !s.GetFollowOnExit() {
		t.Error("followOnExit 默认应为 true")
	}

	if err := s.SetActive("v1.4.9", "installed"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}

	// 重新加载验证落盘
	s2 := newRustDeskStore(dir)
	if v, f := s2.GetActive(); v != "v1.4.9" || f != "installed" {
		t.Errorf("active = %q/%q, want v1.4.9/installed", v, f)
	}
	if s2.GetFollowOnExit() {
		t.Error("followOnExit 应持久化为 false")
	}

	// 清空版本连带清空形态（成对不变式）
	if err := s2.SetActive("", ""); err != nil {
		t.Fatal(err)
	}
	s3 := newRustDeskStore(dir)
	if v, f := s3.GetActive(); v != "" || f != "" {
		t.Errorf("清空后应双空: %q/%q", v, f)
	}
}

func TestStoreCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rustdesk.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRustDeskStore(dir)
	if v, _ := s.GetActive(); v != "" || !s.GetFollowOnExit() {
		t.Errorf("损坏文件应按空配置继续: active=%q follow=%v", v, s.GetFollowOnExit())
	}
}

func TestStoreFollowOnExitBackCompat(t *testing.T) {
	// 老配置无 followOnExit / activeForm 字段 → follow 默认 true，形态按便携兼容
	dir := t.TempDir()
	path := filepath.Join(dir, "rustdesk.json")
	if err := os.WriteFile(path, []byte(`{"activeVersion":"v1.4.8"}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRustDeskStore(dir)
	if v, f := s.GetActive(); v != "v1.4.8" || f != "portable" {
		t.Errorf("旧配置回读错误: active=%q form=%q follow=%v", v, f, s.GetFollowOnExit())
	}
	if !s.GetFollowOnExit() {
		t.Error("followOnExit 默认应为 true")
	}
}
