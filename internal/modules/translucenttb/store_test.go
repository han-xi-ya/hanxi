package translucenttb

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGarbage 向 store 目录写入损坏的 translucenttb.json（模拟半截写入/手工改坏）。
func writeGarbage(dir string) error {
	return os.WriteFile(filepath.Join(dir, "translucenttb.json"), []byte(`{"activeVersion": "20`), 0644)
}

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭），
// 设 true 后落盘重载仍为 true；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newTranslucentTBStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newTranslucentTBStore(dir)
	if !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("2026.2"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
	if got := newTranslucentTBStore(dir).GetActive(); got != "2026.2" {
		t.Fatalf("重载后 activeVersion = %q, want 2026.2", got)
	}
}

// TestStoreCorruptTolerance 损坏文件容忍：解析失败按空配置继续，不阻断模块。
func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := writeGarbage(dir); err != nil {
		t.Fatal(err)
	}
	s := newTranslucentTBStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏配置应退化为默认值，实际 %+v", s)
	}
}
