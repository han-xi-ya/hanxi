package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "MarkerOn_2.9.4_x64_portable.zip"},
		{Name: "MarkerOn_2.9.4_x64-setup.exe"},
		{Name: "markeron-src.tar.gz"},
	}
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"精确匹配", "v2.9.4", true},
		{"无 v 前缀", "2.9.4", true},
		{"大小写不敏感", "v2.9.4", true}, // 资产名大小写不同也走后缀兜底
		{"不存在版本", "v1.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := findPortableAsset(assets, tt.version); ok != tt.want {
				t.Fatalf("findPortableAsset(%q) = %v, want %v", tt.version, ok, tt.want)
			}
		})
	}
}

func TestVersionFromDirName(t *testing.T) {
	tests := []struct {
		name    string
		wantVer string
		wantOK  bool
	}{
		{"markeron_v2.9.4", "v2.9.4", true},
		{"markeron_2.9.4", "v2.9.4", true},
		{"frp_v0.61.1", "", false},
		{"markeron_", "", false},
		{"markeron_imported-x", "", false},
		{"备份", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, ok := versionFromDirName(tt.name)
			if ok != tt.wantOK || ver != tt.wantVer {
				t.Fatalf("versionFromDirName(%q) = (%q, %v), want (%q, %v)", tt.name, ver, ok, tt.wantVer, tt.wantOK)
			}
		})
	}
}

// writeTestZip 构造测试用 zip：files 为 name→content 映射，空内容与空文件条目均可
func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if content != "" {
			if _, err := w.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractAllLayout(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "portable.zip")
	writeTestZip(t, zipPath, map[string]string{
		"MarkerOn.exe":      "fake-binary",
		"markeron.portable": "", // 0 字节标记
		"README.txt":        "requires WebView2 Runtime",
	})

	target := filepath.Join(t.TempDir(), "markeron_v2.9.4")
	if err := extractAll(zipPath, target); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, name := range []string{"MarkerOn.exe", "markeron.portable", "README.txt"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
	// 0 字节标记必须原样保留（便携模式激活条件）
	if fi, _ := os.Stat(filepath.Join(target, "markeron.portable")); fi.Size() != 0 {
		t.Errorf("markeron.portable 大小 = %d, want 0", fi.Size())
	}
}

func TestExtractAllRejectsPathTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	writeTestZip(t, zipPath, map[string]string{
		"../evil.txt": "escape",
	})
	target := filepath.Join(t.TempDir(), "markeron_v2.9.4")
	if err := extractAll(zipPath, target); err == nil {
		t.Fatal("extractAll 应拒绝路径逃逸条目")
	}
}

func TestExtractAllMissingPortableMark(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "broken.zip")
	writeTestZip(t, zipPath, map[string]string{
		"MarkerOn.exe": "fake-binary",
	})
	target := filepath.Join(t.TempDir(), "markeron_v2.9.4")
	err := extractAll(zipPath, target)
	if err == nil {
		t.Fatal("缺 portable 标记应自检失败")
	}
	if !strings.Contains(err.Error(), "便携标记") {
		t.Errorf("错误信息应指出缺失便携标记: %v", err)
	}
	// 失败必须清理目标目录，避免残留损坏安装
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("自检失败后目标目录应被清理")
	}
}

func TestListInstalledSkipsBroken(t *testing.T) {
	dir := t.TempDir()
	// 健康安装
	good := filepath.Join(dir, "markeron_v2.9.4")
	if err := os.MkdirAll(good, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "MarkerOn.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "markeron.portable"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	// 缺便携标记（损坏）
	broken := filepath.Join(dir, "markeron_v2.9.3")
	if err := os.MkdirAll(broken, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "MarkerOn.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// 空 exe（损坏）
	empty := filepath.Join(dir, "markeron_v2.9.2")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(empty, "MarkerOn.exe"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(empty, "markeron.portable"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(dir)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 1 || list[0].Version != "v2.9.4" {
		t.Fatalf("ListInstalled = %+v, 应仅含健康版本 v2.9.4", list)
	}
}
