package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectPortableBaseDir 便携标记目录识别：
// hanxidata 存在即生效；旧包 data 仅在携带数据根特征时兼容；空 data / 无目录均不触发。
func TestDetectPortableBaseDir(t *testing.T) {
	t.Run("hanxidata 空目录即生效", func(t *testing.T) {
		exeDir := t.TempDir()
		want := filepath.Join(exeDir, portableDataDirName)
		if err := os.Mkdir(want, 0755); err != nil {
			t.Fatal(err)
		}
		got, ok := detectPortableBaseDir(exeDir)
		if !ok || got != want {
			t.Fatalf("应命中 %s, got (%q, %v)", portableDataDirName, got, ok)
		}
	})

	t.Run("旧包 data 含 config.json 兼容识别", func(t *testing.T) {
		exeDir := t.TempDir()
		want := filepath.Join(exeDir, legacyPortableDataDirName)
		if err := os.Mkdir(want, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(want, "config.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		got, ok := detectPortableBaseDir(exeDir)
		if !ok || got != want {
			t.Fatalf("真数据根的旧 data 应兼容命中, got (%q, %v)", got, ok)
		}
	})

	t.Run("旧包 data 含 versions 目录兼容识别", func(t *testing.T) {
		exeDir := t.TempDir()
		want := filepath.Join(exeDir, legacyPortableDataDirName)
		if err := os.MkdirAll(filepath.Join(want, "versions"), 0755); err != nil {
			t.Fatal(err)
		}
		got, ok := detectPortableBaseDir(exeDir)
		if !ok || got != want {
			t.Fatalf("含 versions/ 的旧 data 应兼容命中, got (%q, %v)", got, ok)
		}
	})

	t.Run("空 data 目录不触发便携", func(t *testing.T) {
		exeDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(exeDir, legacyPortableDataDirName), 0755); err != nil {
			t.Fatal(err)
		}
		if got, ok := detectPortableBaseDir(exeDir); ok {
			t.Fatalf("空 data 不得切换便携模式, got %q", got)
		}
	})

	t.Run("无任何标记目录走标准模式", func(t *testing.T) {
		exeDir := t.TempDir()
		if _, ok := detectPortableBaseDir(exeDir); ok {
			t.Fatal("干净目录不应命中便携")
		}
	})

	t.Run("hanxidata 优先于旧 data", func(t *testing.T) {
		exeDir := t.TempDir()
		legacy := filepath.Join(exeDir, legacyPortableDataDirName)
		if err := os.Mkdir(legacy, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(exeDir, portableDataDirName)
		if err := os.Mkdir(want, 0755); err != nil {
			t.Fatal(err)
		}
		got, ok := detectPortableBaseDir(exeDir)
		if !ok || got != want {
			t.Fatalf("两标记并存时应首选 hanxidata, got (%q, %v)", got, ok)
		}
	})
}

// TestBuildPaths 数据根派生布局：便携与标准共用同一形态，子目录恒在根下。
func TestBuildPaths(t *testing.T) {
	base := filepath.Join("D:", "Tools", "hanxi", portableDataDirName)
	p := buildPaths(ModePortable, base)

	if p.Mode() != ModePortable || p.BaseDir() != base || p.DataDir() != base {
		t.Fatalf("根目录映射错误: %+v", p)
	}
	for name, got := range map[string]string{
		"logs":     p.LogsDir(),
		"versions": p.VersionsDir(),
		"runtime":  p.RuntimeDir(),
	} {
		if want := filepath.Join(base, name); got != want {
			t.Errorf("%s 子目录派生错误: got %q, want %q", name, got, want)
		}
	}
	if want := filepath.Join(base, "config.json"); p.ConfigFile() != want {
		t.Errorf("ConfigFile 派生错误: got %q, want %q", p.ConfigFile(), want)
	}
}
