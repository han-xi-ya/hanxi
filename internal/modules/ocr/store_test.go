package ocr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDefaults(t *testing.T) {
	s := newOcrStore(t.TempDir())
	if got := s.GetListenPort(); got != defaultListenPort {
		t.Fatalf("默认端口 = %d, want %d", got, defaultListenPort)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("followOnExit 默认应为 true（托管实例不做孤儿）")
	}
	if s.GetExePath() != "" {
		t.Fatal("exePath 默认应为空 = 自动发现")
	}
}

func TestStoreRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	s := newOcrStore(dir)

	if err := s.SetListenPort(99999); err == nil {
		t.Fatal("越界端口应被拒")
	}
	if err := s.SetListenPort(54000); err != nil {
		t.Fatalf("合法端口被拒: %v", err)
	}
	if err := s.SetExePath(`C:\nowhere\hanxi-ocr.exe`); err == nil {
		t.Fatal("不存在的 exe 路径应被拒")
	}
	if err := s.SetExePath(filepath.Join(dir, "notexe.txt")); err == nil {
		t.Fatal("非 .exe 应被拒")
	}

	// 重新加载验证持久化
	s2 := newOcrStore(dir)
	if s2.GetListenPort() != 54000 {
		t.Fatalf("重载端口 = %d, want 54000", s2.GetListenPort())
	}

	// 空串 = 恢复自动发现，合法
	if err := s2.SetExePath(""); err != nil {
		t.Fatalf("清空 exePath 应被允许: %v", err)
	}
	if newOcrStore(dir).GetExePath() != "" {
		t.Fatal("清空后重载仍应为空")
	}
}

func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ocr.json"), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newOcrStore(dir)
	if s.GetListenPort() != defaultListenPort || !s.GetFollowOnExit() {
		t.Fatal("损坏文件应回落默认值继续")
	}
}
