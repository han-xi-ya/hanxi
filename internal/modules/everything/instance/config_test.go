package instance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureHiddenTrayFileMissing(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "Everything.ini")
	if err := ensureHiddenTray(ini); err != nil {
		t.Fatalf("缺文件时创建失败: %v", err)
	}
	b, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "show_tray_icon=0") {
		t.Errorf("新文件应含隐藏托盘键: %q", string(b))
	}
}

func TestEnsureHiddenTrayReplacePreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "Everything.ini")
	content := "[Everything]\r\napp_data=0\r\n  show_tray_icon  =  1\r\nallow_open=1\r\n"
	if err := os.WriteFile(ini, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureHiddenTray(ini); err != nil {
		t.Fatalf("ensureHiddenTray: %v", err)
	}
	b, _ := os.ReadFile(ini)
	got := string(b)
	if !strings.Contains(got, "show_tray_icon  =  0\r\n") {
		t.Errorf("值应原位替换为 0 并保留缩进与 CRLF: %q", got)
	}
	if !strings.Contains(got, "app_data=0\r\n") || !strings.Contains(got, "allow_open=1\r\n") {
		t.Errorf("其他键不得受影响: %q", got)
	}
	if strings.Contains(got, "show_tray_icon  =  1") {
		t.Errorf("旧值 1 残留: %q", got)
	}
	// 再跑一次应幂等（仍为 0，且不追加第二行）
	if err := ensureHiddenTray(ini); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(ini)
	if strings.Count(string(b2), "show_tray_icon") != 1 {
		t.Errorf("幂等失败，键出现 %d 次: %q", strings.Count(string(b2), "show_tray_icon"), string(b2))
	}
}

func TestEnsureHiddenTrayAppendToExisting(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "Everything.ini")
	content := "[Everything]\napp_data=0\n"
	if err := os.WriteFile(ini, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureHiddenTray(ini); err != nil {
		t.Fatalf("ensureHiddenTray: %v", err)
	}
	b, _ := os.ReadFile(ini)
	got := string(b)
	if !strings.HasPrefix(got, content) {
		t.Errorf("原有内容应保持: %q", got)
	}
	if !strings.HasSuffix(got, "show_tray_icon=0\r\n") {
		t.Errorf("末尾应追加隐藏托盘键: %q", got)
	}
}

func TestEnsureHiddenTrayEmptyFile(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "Everything.ini")
	if err := os.WriteFile(ini, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureHiddenTray(ini); err != nil {
		t.Fatalf("空文件写入失败: %v", err)
	}
	b, _ := os.ReadFile(ini)
	if !strings.Contains(string(b), "show_tray_icon=0") {
		t.Errorf("空文件应写入键: %q", string(b))
	}
}