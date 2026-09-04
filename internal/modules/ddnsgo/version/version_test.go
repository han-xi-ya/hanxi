package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 windows x64/arm64/i386 zip、跨平台 tar.gz、预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v6.17.6",
    "published_at": "2026-08-19T12:36:25Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "checksums.txt", "url": "https://api.github.com/x/0", "size": 2804, "digest": "` + h('0') + `"},
      {"name": "ddns-go_6.17.6_windows_arm64.zip", "url": "https://api.github.com/x/1", "size": 4317887, "digest": "` + h('a') + `"},
      {"name": "ddns-go_6.17.6_windows_i386.zip", "url": "https://api.github.com/x/2", "size": 4659482, "digest": "` + h('b') + `"},
      {"name": "ddns-go_6.17.6_linux_x86_64.tar.gz", "url": "https://api.github.com/x/3", "size": 4700000, "digest": "` + h('c') + `"},
      {"name": "ddns-go_6.17.6_windows_x86_64.zip", "url": "https://api.github.com/x/4", "size": 4799520, "digest": "sha256:9d33056f2efff0bbe51987a40c4aa67e1ed2186f8da8e9f7a8236817603f92ff"}
    ]
  },
  {
    "tag_name": "v6.17.5",
    "published_at": "2026-08-10T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.17.5_windows_x86_64.zip", "url": "https://api.github.com/x/5", "size": 4794312, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v6.18.0-beta1",
    "published_at": "2026-08-15T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.18.0-beta1_windows_x86_64.zip", "url": "https://api.github.com/x/6", "size": 4800000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v6.17.4",
    "published_at": "2026-08-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.17.4_windows_x86_64.zip", "url": "https://api.github.com/x/7", "size": 4794324}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：6.17.6/6.17.5 入列表，
// beta tag 丢弃（非纯语义版本），6.17.4 丢弃（缺 digest）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]DdnsRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v6.17.6"]; v.SHA256 != "9d33056f2efff0bbe51987a40c4aa67e1ed2186f8da8e9f7a8236817603f92ff" ||
		v.Size != 4799520 || v.AssetName != "ddns-go_6.17.6_windows_x86_64.zip" || v.IsPre {
		t.Errorf("v6.17.6 解析错误: %+v", v)
	}
	for _, gone := range []string{"v6.18.0-beta1", "v6.17.4"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.Contains(r.AssetName, "arm64") || strings.Contains(r.AssetName, "i386") ||
			strings.Contains(r.AssetName, "tar.gz") {
			t.Errorf("混入非 windows-x64 zip 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：x64 zip 命中、arm64/i386/tar.gz/checksums 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "checksums.txt", Size: 1},
		{Name: "ddns-go_6.17.6_windows_arm64.zip", Size: 1},
		{Name: "ddns-go_6.17.6_windows_i386.zip", Size: 1},
		{Name: "ddns-go_6.17.6_linux_x86_64.tar.gz", Size: 1},
		{Name: "ddns-go_6.17.6_windows_x86_64.zip", Size: 4799520},
	}
	got, ok := findPortableAsset(assets, "v6.17.6")
	if !ok {
		t.Fatal("应命中 windows x64 zip 资产")
	}
	if got.Name != "ddns-go_6.17.6_windows_x86_64.zip" || got.Size != 4799520 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无 windows zip 的 release 不命中
	if _, ok := findPortableAsset([]asset{{Name: "ddns-go_6.17.5_linux_x86_64.tar.gz"}}, "v6.17.5"); ok {
		t.Error("仅有 linux tar.gz 的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "ddnstest-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestExtractAll(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, map[string]string{
		exeName:   "fake-exe",
		"LICENSE": "MIT",
	})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, name := range []string{exeName, "LICENSE"} {
		if _, err := os.Stat(filepath.Join(dir, "dst", name)); err != nil {
			t.Errorf("%s 未解压: %v", name, err)
		}
	}
}

func TestExtractAllZipSlip(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp("", "evil-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../evil.txt")
	w.Write([]byte("evil"))
	w2, _ := zw.Create(exeName)
	w2.Write([]byte("fake"))
	zw.Close()
	f.Close()

	dst := filepath.Join(dir, "dst")
	if err := extractAll(path, dst); err == nil {
		t.Fatal("ZipSlip 条目应被拒绝")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("失败后目标目录应被清理, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("恶意条目逃逸到了目标目录之外")
	}
}

func TestExtractAllMissingExe(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, map[string]string{"LICENSE": "MIT"})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err == nil {
		t.Fatal("缺 exe 的 zip 应被布局自检查拒")
	}
	if _, err := os.Stat(filepath.Join(dir, "dst")); !os.IsNotExist(err) {
		t.Error("自检失败后目标目录应被清理")
	}
}

// TestDirNameRe 目录名规则：语义版本与 imported- 时间戳收纳，其余（如手建杂物目录）拒绝。
func TestDirNameRe(t *testing.T) {
	// 注：字符类与 ccswitch 模板同构（数字起头 + [0-9a-zA-Z.] 续），"ddnsgo_6.17.6.bak"
	// 这类手工杂物目录会被收纳扫描但 exe 缺失即跳过，不构成损坏误判。
	ok := []string{"ddnsgo_6.17.6", "ddnsgo_10.0.1", "ddnsgo_imported-20260819-121314"}
	bad := []string{"ddnsgo_", "ddnsgo_v6.17.6", "ccswitch_3.20.0", "ddnsgo_x"}
	for _, s := range ok {
		if !dirNameRe.MatchString(s) {
			t.Errorf("应接受目录名 %s", s)
		}
	}
	for _, s := range bad {
		if dirNameRe.MatchString(s) {
			t.Errorf("应拒绝目录名 %s", s)
		}
	}
}

// TestResolveVersionDirRejection 非法版本号（路径穿越尝试）必须被拒绝。
func TestResolveVersionDirRejection(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, v := range []string{"../../etc", "v6.17", "6.17.6-extra", ""} {
		if _, err := m.resolveVersionDir(v); err == nil {
			t.Errorf("非法版本号 %q 应被拒绝", v)
		}
	}
}
