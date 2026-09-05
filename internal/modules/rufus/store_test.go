package rufus

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 true（旧配置兼容），
// 设 false 后落盘重载仍为 false；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newRufusStore(dir)
	if !s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 true（随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newRufusStore(dir)
	if s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 false")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("v4.15"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
}

// TestStoreActiveVersionCorruptTolerant 损坏容忍：非法 JSON 按空配置继续，不阻断。
func TestStoreActiveVersionCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rufus.json"), []byte("{-not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRufusStore(dir)
	if s.GetActive() != "" {
		t.Errorf("损坏配置应回落空 activeVersion，实际 %q", s.GetActive())
	}
	if !s.GetFollowOnExit() {
		t.Error("损坏配置下联动开关应回落默认 true")
	}
	// 损坏后仍可正常覆写
	if err := s.SetActive("v4.14"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if newRufusStore(dir).GetActive() != "v4.14" {
		t.Error("覆写后重载应生效")
	}
}
