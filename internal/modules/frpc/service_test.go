package frpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExeDir(t *testing.T) {
	t.Run("valid executable path", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "版本 目录")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		exePath := filepath.Join(dir, "frpc.exe")
		if err := os.WriteFile(exePath, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := resolveExeDir("  " + exePath + "  ")
		if err != nil {
			t.Fatalf("resolveExeDir() error = %v", err)
		}
		if got != dir {
			t.Fatalf("resolveExeDir() = %q, want %q", got, dir)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if _, err := resolveExeDir("  "); err == nil {
			t.Fatal("resolveExeDir() error = nil, want error")
		}
	})

	t.Run("missing executable", func(t *testing.T) {
		exePath := filepath.Join(t.TempDir(), "missing", "frpc.exe")
		if _, err := resolveExeDir(exePath); err == nil {
			t.Fatal("resolveExeDir() error = nil, want error")
		}
		if _, err := os.Stat(exePath); !os.IsNotExist(err) {
			t.Fatalf("missing executable was unexpectedly created: %v", err)
		}
	})

	t.Run("directory path", func(t *testing.T) {
		dir := t.TempDir()
		_, err := resolveExeDir(dir)
		if err == nil {
			t.Fatal("resolveExeDir() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "目标不是 frpc 可执行文件") {
			t.Fatalf("resolveExeDir() error = %q", err)
		}
	})
}
