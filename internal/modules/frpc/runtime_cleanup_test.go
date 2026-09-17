package frpc

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPruneRuntimeConfigs 锁死 S0 修复的清扫边界：只删 runDir 下匹配
// frpc-*.toml 的运行时明文配置，其余文件一律不碰（含同目录其它扩展名、
// 相似前缀与嵌套路径），并确认目录缺失/为空是良性结果。
func TestPruneRuntimeConfigs(t *testing.T) {
	t.Run("removes only matching toml", func(t *testing.T) {
		dir := t.TempDir()
		targets := []string{
			filepath.Join(dir, "frpc-p_1712345678.toml"),
			filepath.Join(dir, "frpc-p_x y.toml"), // 项目 ID 含空格亦须命中
		}
		keeps := []string{
			filepath.Join(dir, "frpc-p_1.toml.tmp"),     // 原子写中间产物不匹配 *.toml
			filepath.Join(dir, "config.toml"),           // 非 frpc- 前缀
			filepath.Join(dir, "frpc-notes.txt"),        // 非 .toml 后缀
			filepath.Join(dir, "sub", "frpc-deep.toml"), // 子目录不递归（运行时配置恒平铺）
		}
		for _, p := range append(targets, keeps...) {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("token=hunter2"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		if err := pruneRuntimeConfigs(dir); err != nil {
			t.Fatalf("pruneRuntimeConfigs() error = %v", err)
		}
		for _, p := range targets {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("运行时配置未擦除: %s (stat err = %v)", p, err)
			}
		}
		for _, p := range keeps {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("误删非目标文件: %s (stat err = %v)", p, err)
			}
		}
	})

	t.Run("missing dir is benign", func(t *testing.T) {
		if err := pruneRuntimeConfigs(filepath.Join(t.TempDir(), "not-exist")); err != nil {
			t.Fatalf("pruneRuntimeConfigs(missing dir) error = %v, want nil", err)
		}
	})

	t.Run("already-removed race tolerated", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "frpc-p_x.toml")
		if err := os.WriteFile(path, []byte("token"), 0o644); err != nil {
			t.Fatal(err)
		}
		// 模拟匹配后被并发删除：Glob 与 Remove 之间文件消失不得报错
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := pruneRuntimeConfigs(dir); err != nil {
			t.Fatalf("pruneRuntimeConfigs() error = %v, want nil on ErrNotExist", err)
		}
	})
}
