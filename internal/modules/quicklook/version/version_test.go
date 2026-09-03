package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖便携 zip、7z/appx/msi/exe 安装资产、"latest" 滚动预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "latest",
    "published_at": "2024-12-06T19:38:07Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "QuickLook-4.5.0-210-g60eea46.zip", "url": "https://api.github.com/x/0", "size": 135000000, "digest": "` + h('0') + `"}
    ]
  },
  {
    "tag_name": "4.5.0",
    "published_at": "2026-04-14T02:49:44Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "QuickLook-4.5.0.7z",  "url": "https://api.github.com/x/1", "size": 62000000, "digest": "` + h('a') + `"},
      {"name": "QuickLook-4.5.0.appx", "url": "https://api.github.com/x/2", "size": 136000000, "digest": "` + h('b') + `"},
      {"name": "QuickLook-4.5.0.exe",  "url": "https://api.github.com/x/3", "size": 64000000, "digest": "` + h('c') + `"},
      {"name": "QuickLook-4.5.0.msi",  "url": "https://api.github.com/x/4", "size": 94000000, "digest": "` + h('d') + `"},
      {"name": "QuickLook-4.5.0.zip",  "url": "https://api.github.com/x/5", "size": 122534056, "digest": "sha256:852d8bcccd984e416fc8491ccedb848f0c3472e930ee0d292188b2ea3df524e0"}
    ]
  },
  {
    "tag_name": "4.4.0",
    "published_at": "2026-01-08T02:27:46Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "QuickLook-4.4.0.msi", "url": "https://api.github.com/x/6", "size": 87000000, "digest": "` + h('e') + `"},
      {"name": "QuickLook-4.4.0.zip", "url": "https://api.github.com/x/7", "size": 113934650, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "3.7.3",
    "published_at": "2022-11-23T22:14:28Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "QuickLook-3.7.3.zip", "url": "https://api.github.com/x/8", "size": 64795788}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：4.5.0 / 4.4.0 入列表；"latest"（非三段 tag）、
// 3.7.3（缺 digest）丢弃；版本无 v 前缀；zip 资产精确命中（排除 7z/msi/exe/appx）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]QuickLookRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if _, ok := byVer["4.5.0"]; !ok {
		t.Fatalf("4.5.0 应入列表")
	}
	if v := byVer["4.5.0"]; v.SHA256 != "852d8bcccd984e416fc8491ccedb848f0c3472e930ee0d292188b2ea3df524e0" ||
		v.Size != 122534056 || v.AssetName != "QuickLook-4.5.0.zip" || v.IsPre {
		t.Errorf("4.5.0 解析错误: %+v", v)
	}
	if _, ok := byVer["latest"]; ok {
		t.Error("\"latest\" 滚动预发布标签不应入列表")
	}
	if _, ok := byVer["3.7.3"]; ok {
		t.Error("缺失官方 digest 的 3.7.3 不应入列表")
	}
	for _, r := range list {
		if !strings.HasSuffix(r.AssetName, ".zip") || strings.Contains(strings.ToLower(r.AssetName), ".msi") {
			t.Errorf("混入非 zip 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：精确命中 QuickLook-<ver>.zip，7z/msi/exe/appx 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "QuickLook-4.5.0.7z", Size: 1},
		{Name: "QuickLook-4.5.0.appx", Size: 1},
		{Name: "QuickLook-4.5.0.exe", Size: 1},
		{Name: "QuickLook-4.5.0.msi", Size: 1},
		{Name: "QuickLook-4.5.0.zip", Size: 122534056},
	}
	got, ok := findPortableAsset(assets, "4.5.0")
	if !ok {
		t.Fatal("应命中便携 zip")
	}
	if got.Name != "QuickLook-4.5.0.zip" || got.Size != 122534056 {
		t.Errorf("命中错误资产: %+v", got)
	}
	if _, ok := findPortableAsset([]asset{{Name: "QuickLook-4.4.0.msi"}}, "4.4.0"); ok {
		t.Error("仅有 msi 的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip（entry 名按原样写入，可含反斜杠以复现官方包布局）。
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "qltest-*.zip")
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
	// 反斜杠分隔的插件子项，复现官方 zip 布局
	zipPath := makeTestZip(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: "",
		nativeMarkName:   "fake-native",
		`QuickLook.Plugin\QuickLook.Plugin.TextViewer\dll`: "plugin",
	})
	dst := filepath.Join(dir, "dst")
	if err := extractAll(zipPath, dst); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, name := range []string{exeName, portableMarkName, nativeMarkName} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s 未解压: %v", name, err)
		}
	}
	// 反斜杠条目应落地为嵌套目录，而非一个含反斜杠的怪文件名
	if _, err := os.Stat(filepath.Join(dst, "QuickLook.Plugin", "QuickLook.Plugin.TextViewer", "dll")); err != nil {
		t.Errorf("反斜杠嵌套条目未正确展开为目录树: %v", err)
	}
}

// TestExtractAllBackslashDirEntries 回归：官方 zip 用反斜杠存目录条目
// （如 QuickLook.Plugin\...\runtimes\），archive/zip 的 IsDir() 只认结尾 "/" 会漏判，
// 旧实现据此把目录 os.Create 成 0 字节同名文件，导致其下文件建目录时报"找不到路径"。
func TestExtractAllBackslashDirEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: "",
		nativeMarkName:   "n",
		`QuickLook.Plugin\QuickLook.Plugin.FontViewer\runtimes\`:                       "",    // 反斜杠结尾目录条目
		`QuickLook.Plugin\QuickLook.Plugin.FontViewer\runtimes\win-x64\native\lib.dll`: "dll", // 其下文件
	})
	dst := filepath.Join(dir, "dst")
	if err := extractAll(zipPath, dst); err != nil {
		t.Fatalf("extractAll 反斜杠目录条目: %v", err)
	}
	rt := filepath.Join(dst, "QuickLook.Plugin", "QuickLook.Plugin.FontViewer", "runtimes")
	if fi, err := os.Stat(rt); err != nil || !fi.IsDir() {
		t.Fatalf("runtimes 应为目录而非误建的 0 字节文件: err=%v", err)
	}
	deep := filepath.Join(rt, "win-x64", "native", "lib.dll")
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("深层文件未正确落地: %v", err)
	}
}

// TestExtractAllRealPortableZip 真机级验证（默认跳过）：设 QL_REAL_ZIP 指向官方
// QuickLook-*.zip 时，用生产 extractAll 全量解压，断言四层完整性之后的布局自检通过，
// 且深层 QuickLook.Plugin\...\runtimes 正确落成目录（正是本次线上失败的场景）。
func TestExtractAllRealPortableZip(t *testing.T) {
	zipPath := os.Getenv("QL_REAL_ZIP")
	if zipPath == "" {
		t.Skip("未设 QL_REAL_ZIP，跳过真包解压验证")
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("真 zip 不存在: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "quicklook_real")
	if err := extractAll(zipPath, dst); err != nil {
		t.Fatalf("真实便携 zip 解压失败: %v", err)
	}
	if err := checkLayout(dst); err != nil {
		t.Fatalf("真实便携 zip 布局自检失败: %v", err)
	}
	rt := filepath.Join(dst, "QuickLook.Plugin", "QuickLook.Plugin.FontViewer", "runtimes")
	if fi, err := os.Stat(rt); err != nil || !fi.IsDir() {
		t.Errorf("FontViewer\\runtimes 未正确落成目录: err=%v", err)
	}
	// 抽查若干深层插件文件确实落地（非被误建的 0 字节同名文件挡路）
	for _, sub := range []string{
		filepath.Join("QuickLook.Plugin", "QuickLook.Plugin.ImageViewer"),
		filepath.Join("QuickLook.Plugin", "QuickLook.Plugin.PdfViewer"),
	} {
		if _, err := os.Stat(filepath.Join(dst, sub)); err != nil {
			t.Errorf("插件目录缺失 %s: %v", sub, err)
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
		"缺 exe":  {portableMarkName: "", nativeMarkName: "n"},
		"缺便携标记":  {exeName: "fake-exe", nativeMarkName: "n"},
		"缺原生组件":  {exeName: "fake-exe", portableMarkName: ""},
		"exe 为空": {exeName: "", portableMarkName: "", nativeMarkName: "n"},
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
		os.WriteFile(filepath.Join(versionsDir, dir, portableMarkName), []byte(""), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("quicklook_4.5.0", `{"installedAt":"2026-08-26 10:00:00","isImport":true,"source":"E:\\QuickLook"}`)
	mkVersion("quicklook_4.4.0", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "quicklook_4.3.0"), 0755) // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]QuickLookVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["4.5.0"]; !v.IsImport || v.InstalledAt != "2026-08-26 10:00:00" || v.Source != "E:\\QuickLook" {
		t.Errorf("4.5.0 元信息解析错误: %+v", v)
	}
	if v := byVer["4.4.0"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("4.4.0 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本（QuickLook 版本号无 v 前缀）
	if exe, err := m.ResolveExe("4.5.0"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(4.5.0): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("4.5.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	// 官方 QuickLook 是整套便携目录：导入应递归迁移全部内容（含插件子树），
	// 与 ccswitch 的单 exe 白名单迁移不同。
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, nativeMarkName), []byte("fake-native"), 0644)
	os.MkdirAll(filepath.Join(src, "QuickLook.Plugin", "TextViewer"), 0755)
	os.WriteFile(filepath.Join(src, "QuickLook.Plugin", "TextViewer", "v.dll"), []byte("x"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion）
	if !strings.HasPrefix(info.Version, "imported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	// 整套迁移：exe / 原生 / 插件子树 / 便携标记兜底补齐，都应在目标目录
	for _, rel := range []string{exeName, nativeMarkName, portableMarkName,
		filepath.Join("QuickLook.Plugin", "TextViewer", "v.dll")} {
		if _, err := os.Stat(filepath.Join(info.Dir, rel)); err != nil {
			t.Errorf("导入后缺少 %s: %v", rel, err)
		}
	}

	// 兜底版本可从目录名解析并支持卸载
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
