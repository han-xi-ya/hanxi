package vscode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newVSCodeStore(dir)

	if s.GetActive() != "" {
		t.Errorf("初始 activeVersion 应为空，实际 %q", s.GetActive())
	}
	if s.GetFollowOnExit() {
		t.Error("默认应不随 Hanxi 关闭（全托管统一口径）")
	}
	if err := s.SetActive("1.136.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}

	// 重新加载验证落盘
	s2 := newVSCodeStore(dir)
	if s2.GetActive() != "1.136.1" || !s2.GetFollowOnExit() {
		t.Errorf("重载错误: %+v / %+v", s2.GetActive(), s2.GetFollowOnExit())
	}

	// 清空设定
	if err := s2.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if newVSCodeStore(dir).GetActive() != "" {
		t.Error("清空未持久化")
	}
}

func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vscode.json"), []byte("{ broken !!!"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newVSCodeStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() {
		t.Errorf("损坏文件应按空配置继续，实际 %+v", s)
	}
	// 损坏后仍可正常写回
	if err := s.SetActive("1.135.0"); err != nil {
		t.Fatalf("损坏态下写回失败: %v", err)
	}
	if newVSCodeStore(dir).GetActive() != "1.135.0" {
		t.Error("写回未生效")
	}
}
