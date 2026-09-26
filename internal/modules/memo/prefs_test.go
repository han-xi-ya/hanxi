package memo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 速记偏好 store 语义（N16 B 批）：出厂默认、指针字段保留"显式停用"、
// 形态校验（无修饰键报错、留空=停用合法）、原子落盘往返、nil 接收者兜底。

func TestQuickPrefsDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	s := newQuickMemoStore(dir)
	if got := s.Hotkey(); got != defaultQuickHotkey {
		t.Fatalf("出厂默认热键 = %q， want %q", got, defaultQuickHotkey)
	}
	if _, err := s.SetHotkey("Ctrl+Alt+M"); err != nil {
		t.Fatal(err)
	}
	reloaded := newQuickMemoStore(dir)
	if got := reloaded.Hotkey(); got != "Ctrl+Alt+M" {
		t.Fatalf("重载热键 = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "memo-prefs.json")); err != nil {
		t.Fatalf("未落盘: %v", err)
	}
}

// 显式停用（空串）必须与"从未设置"可区分：留空落盘后重载仍是留空，
// 不得复活默认键（用户主动停用被无声撤销是配置面的大忌，同 msgboard 纪律）。
func TestQuickPrefsDisabledSticky(t *testing.T) {
	dir := t.TempDir()
	s := newQuickMemoStore(dir)
	if _, err := s.SetHotkey(""); err != nil {
		t.Fatalf("停用热键不应报错: %v", err)
	}
	if got := newQuickMemoStore(dir).Hotkey(); got != "" {
		t.Fatalf("停用态重载 = %q，应为空", got)
	}
}

func TestQuickPrefsValidation(t *testing.T) {
	s := newQuickMemoStore(t.TempDir())
	if _, err := s.SetHotkey("N"); err == nil || !strings.Contains(err.Error(), "修饰键") {
		t.Fatalf("无修饰键应给中文指引: %v", err)
	}
	if got := s.Hotkey(); got != defaultQuickHotkey {
		t.Fatalf("校验失败不得污染内存态: %q", got)
	}
}

func TestQuickPrefsNilSafe(t *testing.T) {
	var s *quickMemoStore
	if got := s.Hotkey(); got != defaultQuickHotkey {
		t.Fatalf("nil store 应回落默认: %q", got)
	}
	if _, err := s.SetHotkey("Ctrl+Alt+X"); err == nil {
		t.Fatal("nil store 落盘应报错而非静默")
	}
}
