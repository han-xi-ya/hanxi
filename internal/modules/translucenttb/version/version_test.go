package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

// fakeFullLayout 官方便携 zip 的完整文件集（2022.1~2026.2 实测恒定布局，测试复用）
func fakeFullLayout() map[string]string {
	files := map[string]string{exeName: "fake-exe"}
	for _, c := range companionNames {
		files[c] = "fake-" + c
	}
	return files
}

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64/arm64 便携、msixbundle/appinstaller/winui 资产、预发布、
// 非规范 tag、缺失 digest 的 release（上游 2025.1 及更早实测无 digest）。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "2026.2",
    "published_at": "2026-08-31T21:01:53Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "bundle.msixbundle", "url": "https://api.github.com/x/1", "size": 4173869, "digest": "` + h('a') + `"},
      {"name": "TranslucentTB-portable-arm64.zip", "url": "https://api.github.com/x/2", "size": 1725478, "digest": "` + h('b') + `"},
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/3", "size": 1769775, "digest": "sha256:0dbe8e0255c20e131cde536dcd0ae490d45989a7d360e26d0150dff1922ac420"},
      {"name": "TranslucentTB.appinstaller", "url": "https://api.github.com/x/4", "size": 2563, "digest": "` + h('c') + `"}
    ]
  },
  {
    "tag_name": "2026.1",
    "published_at": "2026-03-07T21:55:09Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/5", "size": 1765676, "digest": "` + h('d') + `"},
      {"name": "winui-x64.appx", "url": "https://api.github.com/x/6", "size": 4932370, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "2025.2-preview",
    "published_at": "2025-09-01T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/7", "size": 1700000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "nightly-build",
    "published_at": "2025-08-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/8", "size": 1700000, "digest": "` + h('0') + `"}
    ]
  },
  {
    "tag_name": "2025.1",
    "published_at": "2025-04-24T21:09:38Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/9", "size": 1754408}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：2026.2/2026.1 入列表；
// 2025.2-preview 丢弃（tag 非纯 YYYY.N）、nightly-build 丢弃（非规范 tag）、
// 2025.1 丢弃（缺 digest——完整性第一层不能缺位）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]TBRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["2026.2"]; v.SHA256 != "0dbe8e0255c20e131cde536dcd0ae490d45989a7d360e26d0150dff1922ac420" ||
		v.Size != 1769775 || v.AssetName != "TranslucentTB-portable-x64.zip" || v.IsPre {
		t.Errorf("2026.2 解析错误: %+v", v)
	}
	for _, gone := range []string{"2025.2-preview", "nightly-build", "2025.1"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.Contains(strings.ToLower(r.AssetName), "arm64") || strings.HasSuffix(r.AssetName, ".appx") {
			t.Errorf("混入非 x64 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：x64 命中、arm64/msix/appinstaller 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "bundle.msixbundle", Size: 1},
		{Name: "TranslucentTB-portable-arm64.zip", Size: 1},
		{Name: "TranslucentTB.appinstaller", Size: 1},
		{Name: "TranslucentTB-portable-x64.zip", Size: 1769775},
	}
	got, ok := findPortableAsset(assets)
	if !ok {
		t.Fatal("应命中 x64 便携资产")
	}
	if got.Name != "TranslucentTB-portable-x64.zip" || got.Size != 1769775 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 只有 msix/appinstaller 的 release 不命中（2021.5 实测形态）
	if _, ok := findPortableAsset([]asset{{Name: "bundle.msixbundle"}, {Name: "TranslucentTB.appinstaller"}}); ok {
		t.Error("无便携 zip 的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "ttbtest-*.zip")
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

// TestVersionFromToken 版本令牌形状：纯 YYYY.N 与 imported-时间戳收纳（原样返回，
// 无 v 前缀），v 前缀/非年份域/中文目录名拒绝（与原 dirNameRe 口径一致）。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"2026.2", "2026.2", true},
		{"imported-20260906-150405", "imported-20260906-150405", true},
		{"v2026.2", "", false}, // 上游惯例无 v 前缀，v 开头目录非本模块落位格式
		{"2026", "", false},    // 必须 YYYY.N 两段
		{"9.9.9", "", false},   // 非年份域
		{"", "", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK || ver != tt.wantVer {
			t.Errorf("versionFromToken(%q) = (%q,%v), want (%q,%v)", tt.token, ver, ok, tt.wantVer, tt.wantOK)
		}
	}
}

// ---------- 内核解包 + 模块锚点自检（替代原 extractAll 时代的用例） ----------

func TestUnpackWithCompanionAnchors(t *testing.T) {
	entries := fakeFullLayout()
	entries["Assets/SplashScreen.jpeg"] = "jpeg" // 官方便携包附带展示资源，非锚点但必须能全量解出
	entries["README.md"] = "TranslucentTB"
	zipPath := makeTestZip(t, entries)
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkPortableLayout(staging); err != nil {
		t.Fatalf("checkPortableLayout: %v", err)
	}
	for name := range entries {
		if _, err := os.Stat(filepath.Join(staging, filepath.FromSlash(name))); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"../evil.txt": "escape"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

// TestPortableAnchorMissingCompanion 锚点是"全套伴生文件"：exe 之外缺任何一件
// （注入器/UI 线程库/日志/WinUI 页面/资源索引）都判布局无效，错误列明缺项。
func TestPortableAnchorMissingCompanion(t *testing.T) {
	for _, drop := range companionNames {
		entries := fakeFullLayout()
		delete(entries, drop)
		zipPath := makeTestZip(t, entries)
		staging := filepath.Join(t.TempDir(), "staging")
		if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
			t.Fatalf("UnpackZip(%s): %v", drop, err)
		}
		err := checkPortableLayout(staging)
		if err == nil {
			t.Fatalf("缺 %s 应自检失败", drop)
		}
		if !strings.Contains(err.Error(), drop) || !strings.Contains(err.Error(), "伴生") {
			t.Errorf("错误信息应点名列出缺失伴生文件 %s: %v", drop, err)
		}
	}
}

func TestPortableAnchorEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, c := range companionNames {
		if err := os.WriteFile(filepath.Join(staging, c), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
	}
}

// TestNormalizeImportedVersion PE 版本资源归一化：上游 FileVersion 带构建 sha 尾段，
// 须收敛到与远程 tag 同域的 YYYY.N；非规范格式退化时间戳兜底。
func TestNormalizeImportedVersion(t *testing.T) {
	for fv, want := range map[string]string{
		"2026.2.0.d4636e4": "2026.2", // 真实 2026.2 exe 的 FileVersion（实测资源段）
		"2024.4.0.0":       "2024.4",
		"2026.2":           "2026.2",
		"1.0.0.1":          "", // 非年份域 → 走兜底（断言在下方）
		"garbage":          "",
	} {
		if !fileVersionRe.MatchString(strings.TrimSpace(fv)) {
			if want != "" {
				t.Errorf("FileVersion %q 应不匹配", fv)
			}
			continue
		}
		g := fileVersionRe.FindStringSubmatch(strings.TrimSpace(fv))[1]
		if g != want {
			t.Errorf("FileVersion %q 归一化为 %q，期望 %q", fv, g, want)
		}
	}
	// 假 PE 文件读不出版本 → 时间戳兜底格式
	if v := normalizeImportedVersion(filepath.Join(t.TempDir(), "nope.exe")); !importedDirRe.MatchString(v) {
		t.Errorf("探测失败应兜底为 imported-时间戳，实际 %q", v)
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		full := filepath.Join(versionsDir, dir)
		os.MkdirAll(full, 0755)
		for name, content := range fakeFullLayout() {
			os.WriteFile(filepath.Join(full, name), []byte(content), 0644)
		}
		if meta != "" {
			os.WriteFile(filepath.Join(full, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("translucenttb_2026.2", `{"installedAt":"2026-09-01 10:00:00","isImport":true,"source":"E:\\ttb"}`)
	mkVersion("translucenttb_2026.1", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755)      // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "translucenttb_2025.1"), 0755) // 缺 exe 的损坏安装必须跳过
	os.WriteFile(filepath.Join(versionsDir, "translucenttb_2025.1", "Xaml.dll"), []byte("orphan"), 0644)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]TBVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["2026.2"]; !v.IsImport || v.InstalledAt != "2026-09-01 10:00:00" || v.Source != "E:\\ttb" {
		t.Errorf("2026.2 元信息解析错误: %+v", v)
	}
	if v := byVer["2026.1"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("2026.1 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("2026.2"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(2026.2): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("v2026.2"); err == nil {
		t.Error("上游惯例无 v 前缀，v2026.2 应判非法")
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("非年份域版本号应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove（内核 rename 隔离后删除）
	if err := m.Remove("2026.2"); err != nil {
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
	for name, content := range fakeFullLayout() {
		os.WriteFile(filepath.Join(src, name), []byte(content), 0644)
	}
	os.MkdirAll(filepath.Join(src, "Assets"), 0755)
	os.WriteFile(filepath.Join(src, filepath.Join("Assets", "SplashScreen.jpeg")), []byte("jpeg"), 0644)
	os.WriteFile(filepath.Join(src, configName), []byte(`{"desktop":{}}`), 0644)
	os.WriteFile(filepath.Join(src, "readme.txt"), []byte("noise"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会归一化为 2026.2 同域版本）
	if !importedDirRe.MatchString(info.Version) {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	// 全套 + 用户配置 + Assets 都要搬进来
	for _, name := range append(append([]string{exeName}, companionNames...), configName) {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "Assets", "SplashScreen.jpeg")); err != nil {
		t.Errorf("Assets 资源未搬入: %v", err)
	}
	// 非白名单文件（readme）绝不搬运
	if _, err := os.Stat(filepath.Join(info.Dir, "readme.txt")); !os.IsNotExist(err) {
		t.Error("非白名单文件不应被搬运")
	}

	// 兜底版本必须可以从目录名解析并支持卸载（resolveVersionDir 的 imported- 分支）
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

	// 只有 exe、缺伴生文件 → 报错且指明缺项（"半个目录"导进来跑不起来）
	broken := t.TempDir()
	os.WriteFile(filepath.Join(broken, exeName), []byte("fake-exe"), 0644)
	if _, err := m.ImportLocal(broken); err == nil || !strings.Contains(err.Error(), "ExplorerTAP.dll") {
		t.Errorf("缺伴生文件应报错并列明缺项，实际: %v", err)
	}
}

// TestImportLocalWithoutSettings 从未首启过的便携目录（无 settings.json）导入必须成功。
func TestImportLocalWithoutSettings(t *testing.T) {
	m := NewManager(t.TempDir())
	src := t.TempDir()
	for name, content := range fakeFullLayout() {
		os.WriteFile(filepath.Join(src, name), []byte(content), 0644)
	}
	if _, err := m.ImportLocal(src); err != nil {
		t.Fatalf("无 settings.json 的干净便携目录应可导入: %v", err)
	}
}

func TestListingSkipsImportFallbackRecord(t *testing.T) {
	// ImportLocal 的兜底目录名也必须在 ListInstalled 的扫描半径内（可被列出、可被卸载）
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+"imported-20260906-150405")
	os.MkdirAll(dir, 0755)
	for name, content := range fakeFullLayout() {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}
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
