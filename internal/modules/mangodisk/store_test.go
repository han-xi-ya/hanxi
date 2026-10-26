package mangodisk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭），
// 设 true 后落盘重载仍为 true；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newMangoDiskStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newMangoDiskStore(dir)
	if !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("v1.2.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
}

// TestMangoDiskStoreActivePersistAcrossReload T2 补齐：既有测试只回读验证了
// followOnExit 的持久化；activeVersion 必须同样当场写盘、重启可读、清空不复活血。
func TestMangoDiskStoreActivePersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s := newMangoDiskStore(dir)
	if got := s.GetActive(); got != "" {
		t.Fatalf("新目录 active 应为空（未指定），实得 %q", got)
	}
	if err := s.SetActive("v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if got := newMangoDiskStore(dir).GetActive(); got != "v1.2.3" {
		t.Fatalf("重启后 active 应保持，实得 %q", got)
	}
	if err := s.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if got := newMangoDiskStore(dir).GetActive(); got != "" {
		t.Fatalf("清空应持久化（不得被旧盘值复活），实得 %q", got)
	}
}

func TestMangoDiskStoreCorruptFileTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mangodisk.json")
	if err := os.WriteFile(path, []byte("{损坏不是JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newMangoDiskStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏账本应按空配置继续，实得 %q/%v", s.GetActive(), s.GetFollowOnExit())
	}
	if err := s.SetActive("v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if got := newMangoDiskStore(dir).GetActive(); got != "v1.2.3" {
		t.Fatalf("损坏后重写应可回读，实得 %q", got)
	}
}
