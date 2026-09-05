package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖多平台多形态资产、前导零 tag（v2.00.7）、rc 预发布、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v2.15.0",
    "published_at": "2026-08-19T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Bili23-Downloader_2.15.0_linux_amd64_portable.tar.gz", "url": "https://api.github.com/x/1", "size": 67133753, "digest": "` + h('a') + `"},
      {"name": "Bili23-Downloader_2.15.0_macos_aarch64.dmg", "url": "https://api.github.com/x/2", "size": 62971532, "digest": "` + h('b') + `"},
      {"name": "Bili23-Downloader_2.15.0_windows_x64.exe", "url": "https://api.github.com/x/3", "size": 33858934, "digest": "` + h('c') + `"},
      {"name": "Bili23-Downloader_2.15.0_windows_x64_for_win7.exe", "url": "https://api.github.com/x/4", "size": 34190600, "digest": "` + h('d') + `"},
      {"name": "Bili23-Downloader_2.15.0_windows_x64_portable.zip", "url": "https://api.github.com/x/5", "size": 43386993, "digest": "sha256:c233d9ca04c1840340a3594df323f466c82a697874f7de9a6c6cc327f9141c59"}
    ]
  },
  {
    "tag_name": "v2.00.7",
    "published_at": "2026-06-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Bili23-Downloader_2.00.7_windows_x64_portable.zip", "url": "https://api.github.com/x/6", "size": 41655016, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v2.00.5-rc1",
    "published_at": "2026-05-20T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "Bili23-Downloader_2.00.5-rc1_windows_x64_portable.zip", "url": "https://api.github.com/x/7", "size": 41000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v2.00.6",
    "published_at": "2026-05-28T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Bili23-Downloader_2.00.6_windows_x64_portable.zip", "url": "https://api.github.com/x/8", "size": 41200000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：v2.15.0 与 v2.00.7（前导零变体）入列表；
// rc tag（v2.00.5-rc1）与缺 digest（v2.00.6）一律丢弃。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]Bili23Release{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v2.15.0"]; v.SHA256 != "c233d9ca04c1840340a3594df323f466c82a697874f7de9a6c6cc327f9141c59" ||
		v.Size != 43386993 || v.AssetName != "Bili23-Downloader_2.15.0_windows_x64_portable.zip" {
		t.Errorf("v2.15.0 解析错误: %+v", v)
	}
	if _, ok := byVer["v2.00.7"]; !ok {
		t.Error("前导零 tag v2.00.7 应入列表（上游历史版本真实存在）")
	}
	for _, gone := range []string{"v2.00.5-rc1", "v2.00.6"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if !strings.HasSuffix(strings.ToLower(r.AssetName), "_windows_x64_portable.zip") {
			t.Errorf("混入非 windows 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：多形态资产中只命中 x64 portable zip。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "Bili23-Downloader_2.15.0_linux_amd64_portable.tar.gz", Size: 1},
		{Name: "Bili23-Downloader_2.15.0_windows_x64.exe", Size: 1},
		{Name: "Bili23-Downloader_2.15.0_windows_x64_for_win7.exe", Size: 1},
		{Name: "Bili23-Downloader_2.15.0_windows_x64_portable.zip", Size: 43386993},
	}
	got, ok := findPortableAsset(assets, "v2.15.0")
	if !ok {
		t.Fatal("应命中 x64 便携资产")
	}
	if got.Name != "Bili23-Downloader_2.15.0_windows_x64_portable.zip" || got.Size != 43386993 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无便携资产的 release 不命中（win7 安装包不是便携形态）
	if _, ok := findPortableAsset([]asset{{Name: "Bili23-Downloader_2.14.0_windows_x64.exe"}}, "v2.14.0"); ok {
		t.Error("仅有安装包的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "bili23test-*.zip")
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

// bili23ZipEntries 官方便携包同构布局：顶层单目录 + 三锚点 + 运行时子目录。
func bili23ZipEntries() map[string]string {
	return map[string]string{
		topDirName + "/":                      "",
		topDirName + "/Bili23.exe":            "fake-exe",
		topDirName + "/_pystand_static.int":   "bootstrap",
		topDirName + "/LICENSE":               "gpl",
		topDirName + "/script/main.py":        "def _main(): pass",
		topDirName + "/runtime/python313.dll": "dll",
		topDirName + "/bundle/ffmpeg.exe":     "ffmpeg",
	}
}

// TestExtractAllFlattensTopDir 解压必须剥离顶层 Bili23-Downloader/ 使 exe 落在隔离目录根。
func TestExtractAllFlattensTopDir(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, bili23ZipEntries())
	dst := filepath.Join(dir, "dst")
	if err := extractAll(zipPath, dst); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, rel := range []string{exeName, bootstrapName, filepath.FromSlash(scriptMainRel), filepath.Join("runtime", "python313.dll")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s 未解压到根: %v", rel, err)
		}
	}
	// 顶层目录残影绝不允许出现
	if _, err := os.Stat(filepath.Join(dst, topDirName)); !os.IsNotExist(err) {
		t.Error("顶层目录未被剥离")
	}
}

// TestExtractAllFlatLayoutCompat 上游若某天改为扁平布局，同样要能装。
func TestExtractAllFlatLayoutCompat(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		exeName:                         "fake-exe",
		bootstrapName:                   "bootstrap",
		filepath.ToSlash(scriptMainRel): "def _main(): pass",
	})
	dst := filepath.Join(t.TempDir(), "dst")
	if err := extractAll(zipPath, dst); err != nil {
		t.Fatalf("扁平布局 extractAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, exeName)); err != nil {
		t.Errorf("exe 未解压: %v", err)
	}
}

func TestExtractAllZipSlip(t *testing.T) {
	cases := map[string][]string{
		"根级逃逸":  {"../evil.txt", exeName, bootstrapName, scriptMainRel},
		"顶层内逃逸": {topDirName + "/../../evil.txt", topDirName + "/" + exeName, topDirName + "/" + bootstrapName, topDirName + "/" + scriptMainRel},
	}
	for name, entries := range cases {
		zipPath := makeTestZip(t, zipMap(entries))
		dst := filepath.Join(t.TempDir(), "dst")
		if err := extractAll(zipPath, dst); err == nil {
			t.Fatalf("%s: ZipSlip 条目应被拒绝", name)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("%s: 失败后目标目录应被清理", name)
		}
	}
}

func zipMap(names []string) map[string]string {
	m := map[string]string{}
	for _, n := range names {
		m[n] = "x"
	}
	// exe 锚点不能为空内容（verifyLayout 拒绝零字节 exe）
	for k := range m {
		if strings.HasSuffix(k, exeName) {
			m[k] = "fake-exe"
		}
	}
	return m
}

func TestExtractAllMissingBits(t *testing.T) {
	cases := map[string]map[string]string{
		"缺引导脚本": {
			topDirName + "/Bili23.exe":     "fake-exe",
			topDirName + "/script/main.py": "x",
		},
		"缺主模块": {
			topDirName + "/Bili23.exe":          "fake-exe",
			topDirName + "/_pystand_static.int": "x",
		},
		"exe 为空": {
			topDirName + "/Bili23.exe":          "",
			topDirName + "/_pystand_static.int": "x",
			topDirName + "/script/main.py":      "x",
		},
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

// mkInstalledVersion 在 versionsDir 下构造一个布局齐备的伪安装
func mkInstalledVersion(t *testing.T, versionsDir, dirName, meta string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, dirName)
	os.MkdirAll(filepath.Join(dir, "script", "util", "common"), 0755)
	os.WriteFile(filepath.Join(dir, exeName), []byte("fake-exe"), 0644)
	os.WriteFile(filepath.Join(dir, bootstrapName), []byte("bootstrap"), 0644)
	os.WriteFile(filepath.Join(dir, "script", "main.py"), []byte("def _main(): pass"), 0644)
	os.WriteFile(filepath.Join(dir, "script", "util", "common", "config.py"),
		[]byte("class Config:\n    app_version = \"2.15.0\"\n"), 0644)
	if meta != "" {
		os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644)
	}
	return dir
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkInstalledVersion(t, versionsDir, "bili23_2.15.0", `{"installedAt":"2026-08-26 10:00:00","isImport":true,"source":"E:\\bili23"}`)
	mkInstalledVersion(t, versionsDir, "bili23_2.00.7", "")
	mkInstalledVersion(t, versionsDir, "ccswitch_3.20.0", "") // 异模块目录必须跳过（即便布局伪齐）
	// 损坏安装：缺 script/main.py
	broken := mkInstalledVersion(t, versionsDir, "bili23_2.10.0", "")
	os.Remove(filepath.Join(broken, "script", "main.py"))

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]Bili23VersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v2.15.0"]; !v.IsImport || v.InstalledAt != "2026-08-26 10:00:00" || v.Source != "E:\\bili23" {
		t.Errorf("2.15 元信息解析错误: %+v", v)
	}
	if v := byVer["v2.15.0"]; v.Size <= 0 {
		t.Errorf("目录总大小应为正数: %+v", v)
	}
	if v := byVer["v2.00.7"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("2.00.7 默认元信息错误: %+v", v)
	}

	if exe, err := m.ResolveExe("v2.15.0"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v2.15.0): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("2.00.7"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("v2.15.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

// TestImportLocalFullTree 导入是整目录复制：三锚点 + app_version 解析 + 嵌套目录自动下钻。
func TestImportLocalFullTree(t *testing.T) {
	m := NewManager(t.TempDir())

	// 源目录：顶层嵌套 Bili23-Downloader/（模拟用户手动解压 zip 后的外层目录）
	outer := t.TempDir()
	src := filepath.Join(outer, topDirName)
	mkInstalledVersion(t, outer, topDirName, "")
	os.WriteFile(filepath.Join(src, "readme.txt"), []byte("noise"), 0644)

	info, err := m.ImportLocal(outer)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// config.py 的 app_version 被正确解析（假安装构造时写入 2.15.0）
	if info.Version != "v2.15.0" {
		t.Errorf("app_version 解析失败: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记/自动下钻错误: %+v", info)
	}
	for _, rel := range []string{exeName, bootstrapName, filepath.FromSlash(scriptMainRel), "readme.txt"} {
		if _, err := os.Stat(filepath.Join(info.Dir, rel)); err != nil {
			t.Errorf("导入后缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, topDirName)); !os.IsNotExist(err) {
		t.Error("导入不应保留嵌套顶层目录")
	}

	// 重复导入同版本应被拒绝
	if _, err := m.ImportLocal(outer); err == nil {
		t.Error("重复导入应报错")
	}

	// 无有效布局的目录 → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("空目录应报错")
	}
}

// TestImportLocalVersionFallback config.py 缺失/变更时走时间戳兜底目录。
func TestImportLocalVersionFallback(t *testing.T) {
	m := NewManager(t.TempDir())
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "script"), 0755)
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe"), 0644)
	os.WriteFile(filepath.Join(src, bootstrapName), []byte("bootstrap"), 0644)
	os.WriteFile(filepath.Join(src, "script", "main.py"), []byte("x"), 0644)
	// 无 script/util/common/config.py → 版本探测失败

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("应走时间戳兜底: %q", info.Version)
	}
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}
	if err := m.Remove(info.Version); err != nil {
		t.Errorf("兜底版本应可卸载: %v", err)
	}
}

// TestImportLocalRejectsManagedSource 不允许从 Hanxi 托管目录自我导入。
func TestImportLocalRejectsManagedSource(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	dir := mkInstalledVersion(t, versionsDir, "bili23_2.14.0", "")

	if _, err := m.ImportLocal(dir); err == nil || !strings.Contains(err.Error(), "托管") {
		t.Errorf("托管目录自我导入应被拒绝: %v", err)
	}
}

// TestDetectAppVersion 解析上游 config.py 的 app_version 常量。
func TestDetectAppVersion(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "script", "util", "common"), 0755)
	p := filepath.Join(dir, "script", "util", "common", "config.py")

	os.WriteFile(p, []byte(`class Config:
    app_version = "2.15.0"
`), 0644)
	if got := detectAppVersion(dir); got != "2.15.0" {
		t.Errorf("解析错误: %q", got)
	}

	os.WriteFile(p, []byte(`    app_version = '2.00.7'`), 0644)
	if got := detectAppVersion(dir); got != "2.00.7" {
		t.Errorf("前导零解析错误: %q", got)
	}

	os.WriteFile(p, []byte(`    app_version = get_version()`), 0644)
	if got := detectAppVersion(dir); got != "" {
		t.Errorf("非字面量应返回空: %q", got)
	}
}
