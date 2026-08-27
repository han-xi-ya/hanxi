package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64/arm64 便携、msi/sig 安装资产、预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v3.20.0",
    "published_at": "2026-08-18T09:11:14Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "CC-Switch-v3.20.0-Windows-arm64-Portable.zip", "url": "https://api.github.com/x/1", "size": 13000000, "digest": "` + h('a') + `"},
      {"name": "CC-Switch-v3.20.0-Windows.msi", "url": "https://api.github.com/x/2", "size": 13000000, "digest": "` + h('b') + `"},
      {"name": "CC-Switch-v3.20.0-Windows-Portable.zip", "url": "https://api.github.com/x/3", "size": 13581534, "digest": "sha256:cc37942f63a40c7ba57749d413d0da4c6347db2a29205f6d6e02ec86617d208a"}
    ]
  },
  {
    "tag_name": "v3.19.0",
    "published_at": "2026-07-30T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "CC-Switch-v3.19.0-Windows.msi", "url": "https://api.github.com/x/4", "size": 13000000, "digest": "` + h('c') + `"},
      {"name": "CC-Switch-v3.19.0-Windows-Portable.zip", "url": "https://api.github.com/x/5", "size": 13000000, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v3.18.0",
    "published_at": "2026-07-01T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "CC-Switch-v3.18.0-Windows-Portable.zip", "url": "https://api.github.com/x/6", "size": 13000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "nightly-build",
    "published_at": "2026-07-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "CC-Switch-nightly-build-Windows-Portable.zip", "url": "https://api.github.com/x/7", "size": 13000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v3.17.0",
    "published_at": "2026-06-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "CC-Switch-v3.17.0-Windows-Portable.zip", "url": "https://api.github.com/x/8", "size": 13000000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：3.20/3.19 入列表（假 sha 可入），
// 预发布 3.18 保留但 IsPre，nightly-build 丢弃（tag 非规范），3.17 丢弃（缺 digest）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]CCRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v3.20.0"]; v.SHA256 != "cc37942f63a40c7ba57749d413d0da4c6347db2a29205f6d6e02ec86617d208a" ||
		v.Size != 13581534 || v.IsPre {
		t.Errorf("v3.20.0 解析错误: %+v", v)
	}
	if v, ok := byVer["v3.18.0"]; !ok || !v.IsPre {
		t.Errorf("预发布版本应保留并标记 IsPre: %+v", v)
	}
	for _, gone := range []string{"nightly-build", "v3.17.0"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.HasPrefix(r.AssetName, "CC-Switch-") && (strings.Contains(r.AssetName, "arm64") || strings.Contains(r.AssetName, "msi")) {
			t.Errorf("混入非 x64 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：x64 命中、arm64/msi/sig 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "CC-Switch-v3.20.0-Windows-arm64-Portable.zip", Size: 1},
		{Name: "CC-Switch-v3.20.0-Windows.msi", Size: 1},
		{Name: "CC-Switch-v3.20.0-Windows.msi.sig", Size: 1},
		{Name: "CC-Switch-v3.20.0-Windows-Portable.zip", Size: 13581534},
	}
	got, ok := findPortableAsset(assets, "v3.20.0")
	if !ok {
		t.Fatal("应命中 x64 便携资产")
	}
	if got.Name != "CC-Switch-v3.20.0-Windows-Portable.zip" || got.Size != 13581534 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无便携资产的 release 不命中
	if _, ok := findPortableAsset([]asset{{Name: "CC-Switch-v3.19.0-Windows.msi"}}, "v3.19.0"); ok {
		t.Error("仅有 msi 的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "cctest-*.zip")
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
		exeName:          "fake-exe",
		portableMarkName: "portable=true\n",
	})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, name := range []string{exeName, portableMarkName} {
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

func TestExtractAllMissingBits(t *testing.T) {
	cases := map[string]map[string]string{
		"缺 exe":  {portableMarkName: "portable=true\n"},
		"缺便携标记":  {exeName: "fake-exe"},
		"exe 为空": {exeName: "", portableMarkName: "portable=true\n"},
	}
	for name, entries := range cases {
		zipPath := makeTestZip(t, entries)
		dst := filepath.Join(t.TempDir(), "dst")
		if err := extractAll(zipPath, dst); err == nil {
			t.Errorf("%s: 应被拒绝", name)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("%s: 失败后目标目录应被清理", name)
		}
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		os.MkdirAll(filepath.Join(versionsDir, dir), 0755)
		os.WriteFile(filepath.Join(versionsDir, dir, exeName), []byte("fake-exe"), 0644)
		os.WriteFile(filepath.Join(versionsDir, dir, portableMarkName), []byte("portable=true"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("ccswitch_3.20.0", `{"installedAt":"2026-08-26 10:00:00","isImport":true,"source":"E:\\cc-switch"}`)
	mkVersion("ccswitch_3.18.2", "")
	os.MkdirAll(filepath.Join(versionsDir, "markeron_v2.9.4"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.19.0"), 0755) // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]CCVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v3.20.0"]; !v.IsImport || v.InstalledAt != "2026-08-26 10:00:00" || v.Source != "E:\\cc-switch" {
		t.Errorf("3.20 元信息解析错误: %+v", v)
	}
	if v := byVer["v3.18.2"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("3.18 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("v3.20.0"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v3.20.0): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("3.20.0"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("v3.20.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, portableMarkName), []byte("portable=true"), 0644)
	os.WriteFile(filepath.Join(src, "readme.txt"), []byte("noise"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion）
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	for _, name := range []string{exeName, portableMarkName} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	// 非白名单文件（readme）绝不搬运
	if _, err := os.Stat(filepath.Join(info.Dir, "readme.txt")); !os.IsNotExist(err) {
		t.Error("非白名单文件不应被搬运")
	}

	// 兜底版本必须可以从目录名解析并支持卸载（resolveVersionDir 的 imported- 分支）
	ver := strings.TrimPrefix(info.Version, "v")
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("兜底版本目录不存在: %v", err)
	}
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}

	// 重复导入同一版本应被拒绝
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("重复导入应报错")
	}

	// 源目录不含 exe → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 exe 的目录应报错")
	}
}

func TestListingSkipsImportFallbackRecord(t *testing.T) {
	// ImportLocal 的兜底目录名也必须在 ListInstalled 的扫描半径内（可被列出、可被卸载）
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+"imported-20260826-150405")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, exeName), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(dir, portableMarkName), []byte("portable=true"), 0644)
	m := NewManager(versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("兜底目录应被列出，实际 %d: %+v", len(list), list)
	}
	if err := m.Remove(list[0].Version); err != nil {
		t.Errorf("兜底版本应可卸载: %v", err)
	}
}
