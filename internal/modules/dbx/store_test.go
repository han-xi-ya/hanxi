package dbx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭），
// 设 true 后落盘重载仍为 true；存取与 ActiveVersion / DataDirInjected 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newDBXStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newDBXStore(dir)
	if !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("v1.5.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
}

// TestDataDirInjectedPersist 注入事实账：默认 false；置 true 落盘重载仍真
// （metaHints/GetStatus 披露的持久依据）；重复置真幂等、不清其它字段。
func TestDataDirInjectedPersist(t *testing.T) {
	dir := t.TempDir()

	s := newDBXStore(dir)
	if s.GetDataDirInjected() {
		t.Fatal("新 store 的 dataDirInjected 默认应为 false（尚未发生过注入式托管启动）")
	}

	if err := s.SetDataDirInjected(true); err != nil {
		t.Fatalf("SetDataDirInjected: %v", err)
	}
	if err := s.SetDataDirInjected(true); err != nil {
		t.Fatalf("重复置真应幂等成功: %v", err)
	}
	if err := s.SetActive("v1.5.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	s2 := newDBXStore(dir)
	if !s2.GetDataDirInjected() {
		t.Fatal("重载后 dataDirInjected 应保持 true")
	}
	if s2.GetActive() != "v1.5.3" {
		t.Fatalf("重载后 activeVersion 应保持: %q", s2.GetActive())
	}
}

// TestStoreCorruptionTolerance 损坏容忍：非法 JSON 落盘后按空配置继续
// （jsonstore 收口纪律的 dbx 侧回归位）。
func TestStoreCorruptionTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := newDBXStore(dir).SetDataDirInjected(true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dbx.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	s := newDBXStore(dir)
	if s.GetDataDirInjected() || s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏文件应回落空配置: %+v", s)
	}
}
