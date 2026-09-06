package everything

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newEverythingStore(dir)
	if got := s.GetActive(); got != "" {
		t.Fatalf("初始应未指定版本，实际 %q", got)
	}

	if err := s.SetActive("1.5.0.1422b"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got := s.GetActive(); got != "1.5.0.1422b" {
		t.Fatalf("回读版本 = %q", got)
	}

	// 落盘文件存在且重新加载可见（验证原子写生效）
	if _, err := os.Stat(filepath.Join(dir, "everything.json")); err != nil {
		t.Fatalf("配置文件未落盘: %v", err)
	}
	s2 := newEverythingStore(dir)
	if got := s2.GetActive(); got != "1.5.0.1422b" {
		t.Fatalf("重载版本 = %q", got)
	}
}

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭），
// 设 true 后落盘重载仍为 true；存取与 activeVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newEverythingStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	if s2 := newEverythingStore(dir); !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("1.5.0.1422b"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
}

func TestStoreCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "everything.json"), []byte("{corrupt json"), 0644)
	s := newEverythingStore(dir)
	if got := s.GetActive(); got != "" {
		t.Fatalf("损坏配置应回退空版本，实际 %q", got)
	}
	// 损坏不阻断后续写入
	if err := s.SetActive("1.4.1.1032"); err != nil {
		t.Fatalf("损坏后写入: %v", err)
	}
}
