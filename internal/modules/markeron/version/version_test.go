package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
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

func TestParseAssetDigest(t *testing.T) {
	tests := []struct {
		raw     string
		wantOK  bool
		wantHex string
	}{
		{"sha256:71e86b4a97979bc5ce0f078100286c69c97b9512c71e61865bd02b608bebda19", true, "71e86b4a97979bc5ce0f078100286c69c97b9512c71e61865bd02b608bebda19"},
		{"SHA256:AB00", false, ""}, // 长度不足
		{"", false, ""},
		{"sha512:" + strings.Repeat("a", 128), false, ""}, // 非 sha256 算法拒用
		{"garbage", false, ""},
		{"sha256:" + strings.Repeat("f", 64), true, strings.Repeat("f", 64)},
	}
	for _, tt := range tests {
		algo, hex64, ok := parseAssetDigest(tt.raw)
		if ok != tt.wantOK {
			t.Errorf("parseAssetDigest(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
		}
		if ok && (algo != "sha256" || hex64 != tt.wantHex) {
			t.Errorf("parseAssetDigest(%q) = (%q,%q)", tt.raw, algo, hex64)
		}
	}
}

func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		name    string
		wantVer string
		wantOK  bool
	}{
		{"2.9.4", "v2.9.4", true},
		{"v2.9.4", "v2.9.4", true}, // 历史目录名（markeron_vX.Y.Z）兼容
		{"imported-x", "", false},  // 非版本形状的外来目录
		{"", "", false},
		{"2.9", "", false}, // 必须纯 x.y.z
		{"备份", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, ok := versionFromToken(tt.name)
			if ok != tt.wantOK || ver != tt.wantVer {
				t.Fatalf("versionFromToken(%q) = (%q, %v), want (%q, %v)", tt.name, ver, ok, tt.wantVer, tt.wantOK)
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

// TestUnpackWithPortableAnchor 内核解包 + 模块锚点自检的正例：
// 布局完整（exe、0 字节 portable 标记、README）时自检通过，0 字节标记原样保留。
func TestUnpackWithPortableAnchor(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "portable.zip")
	writeTestZip(t, zipPath, map[string]string{
		"MarkerOn.exe":      "fake-binary",
		"markeron.portable": "", // 0 字节标记
		"README.txt":        "requires WebView2 Runtime",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkPortableLayout(staging); err != nil {
		t.Fatalf("checkPortableLayout: %v", err)
	}
	for _, name := range []string{"MarkerOn.exe", "markeron.portable", "README.txt"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
	// 0 字节标记必须原样保留（便携模式激活条件）
	if fi, _ := os.Stat(filepath.Join(staging, "markeron.portable")); fi.Size() != 0 {
		t.Errorf("markeron.portable 大小 = %d, want 0", fi.Size())
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	writeTestZip(t, zipPath, map[string]string{
		"../evil.txt": "escape",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

func TestPortableAnchorMissingMark(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "broken.zip")
	writeTestZip(t, zipPath, map[string]string{
		"MarkerOn.exe": "fake-binary",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	err := checkPortableLayout(staging)
	if err == nil {
		t.Fatal("缺 portable 标记应自检失败")
	}
	if !strings.Contains(err.Error(), "便携标记") {
		t.Errorf("错误信息应指出缺失便携标记: %v", err)
	}
}

func TestPortableAnchorEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, portableMarkName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
	}
}

// installFakeVersion 在版本树里直造一个已装版本目录（绕过下载链，供扫描/卸载测试）
func installFakeVersion(t *testing.T, root, dirName, exeContent string, withMark bool) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(exeContent), 0644); err != nil {
		t.Fatal(err)
	}
	if withMark {
		if err := os.WriteFile(filepath.Join(dir, portableMarkName), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestListInstalledSkipsBroken(t *testing.T) {
	dir := t.TempDir()
	installFakeVersion(t, dir, "markeron_2.9.4", "x", true)                  // 健康安装（当前落位格式）
	installFakeVersion(t, dir, "markeron_v1.0.0", "x", true)                 // 健康安装（历史 v 前缀格式）
	installFakeVersion(t, dir, "markeron_2.9.3", "x", false)                 // 缺便携标记（损坏）
	installFakeVersion(t, dir, "markeron_2.9.2", "", true)                   // 空 exe（损坏）
	installFakeVersion(t, dir, "markeron_imported-x", "x", true)             // 版本形状外，不列入
	installFakeVersion(t, dir, "frpc_0.61.1", "x", true)                     // 他模块目录，不干涉
	if _, err := os.Stat(filepath.Join(dir, "markeron_2.9.2")); err != nil { // sanity
		t.Fatal(err)
	}

	m := NewManager(dir)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	got := map[string]bool{}
	for _, v := range list {
		got[v.Version] = true
	}
	if len(list) != 2 || !got["v2.9.4"] || !got["v1.0.0"] {
		t.Fatalf("ListInstalled = %+v, 应仅含 v2.9.4 与 v1.0.0", list)
	}
}

func TestResolveExeLayoutCompat(t *testing.T) {
	dir := t.TempDir()
	installFakeVersion(t, dir, "markeron_2.9.4", "x", true)
	installFakeVersion(t, dir, "markeron_v1.0.0", "x", true)
	m := NewManager(dir)

	for _, want := range []struct{ ver, dirName string }{
		{"v2.9.4", "markeron_2.9.4"},
		{"2.9.4", "markeron_2.9.4"},
		{"v1.0.0", "markeron_v1.0.0"}, // 历史目录名回退
	} {
		exe, err := m.ResolveExe(want.ver)
		if err != nil {
			t.Fatalf("ResolveExe(%q): %v", want.ver, err)
		}
		if filepath.Base(filepath.Dir(exe)) != want.dirName {
			t.Errorf("ResolveExe(%q) = %q, want dir %s", want.ver, exe, want.dirName)
		}
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	} else if !strings.Contains(err.Error(), "未安装") {
		t.Errorf("错误文案应保持引导口径: %v", err)
	}
}
