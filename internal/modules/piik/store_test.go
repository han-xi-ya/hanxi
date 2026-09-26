package piik

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPiikStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newPiikStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("新库默认应 active 空 / followOnExit false: %q %v", s.GetActive(), s.GetFollowOnExit())
	}
	if err := s.SetActive("v1.6.5"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}

	// 重开同一文件必须读到同一份账（落盘口径而非内存口径）
	s2 := newPiikStore(dir)
	if s2.GetActive() != "v1.6.5" || !s2.GetFollowOnExit() {
		t.Fatalf("重载回转失败: %q %v", s2.GetActive(), s2.GetFollowOnExit())
	}
	if got := filepath.Base(s2.filePath); got != "piik.json" {
		t.Fatalf("状态文件位置/命名异常: %s", s2.filePath)
	}
}

func TestPiikStoreEmptyVersionRoundTripsAsEmpty(t *testing.T) {
	dir := t.TempDir()
	s := newPiikStore(dir)
	if err := s.SetActive("v1.6.5"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	// 末版卸载清空设定（RemoveVersion 的清账路径依赖本语义）
	if err := s.SetActive(""); err != nil {
		t.Fatalf("SetActive 清空: %v", err)
	}
	if got := newPiikStore(dir).GetActive(); got != "" {
		t.Fatalf("清空应落盘（重载仍为空）, got %q", got)
	}
}

func TestPiikStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "piik.json")
	if err := os.WriteFile(path, []byte("{ 这不是合法 JSON"), 0o600); err != nil {
		t.Fatalf("写入损坏文件: %v", err)
	}
	s := newPiikStore(dir)
	// 损坏容忍：按空配置继续，不阻断模块（activeVersion 自然兜底"自动最新已装"）
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Fatalf("损坏文件应按空配置兜底: %q %v", s.GetActive(), s.GetFollowOnExit())
	}
	if err := s.SetActive("v1.6.5"); err != nil {
		t.Fatalf("损坏后仍应可写回: %v", err)
	}
	if got := newPiikStore(dir).GetActive(); got != "v1.6.5" {
		t.Fatalf("写回后重载异常: %q", got)
	}
}
