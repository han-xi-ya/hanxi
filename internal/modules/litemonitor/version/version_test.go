package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64 便携、arm64/安装包/sig 变体、预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v1.3.6",
    "published_at": "2026-05-15T19:56:07Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "LiteMonitor_v1.3.6-win-arm64.zip", "url": "https://api.github.com/x/1", "size": 2800000, "digest": "` + h('a') + `"},
      {"name": "LiteMonitor_v1.3.6-win-x64.zip", "url": "https://api.github.com/x/2", "size": 2901234, "digest": "sha256:966c60e0e327d995f8f6edf59c630331a24fdc7aeb4c5765ab539cbf6a6471d7"}
    ]
  },
  {
    "tag_name": "v1.3.5",
    "published_at": "2026-05-14T22:46:05Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "LiteMonitor_v1.3.5-win-x64.zip", "url": "https://api.github.com/x/3", "size": 2890000, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v1.4.0-beta1",
    "published_at": "2026-05-01T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "LiteMonitor_v1.4.0-beta1-win-x64.zip", "url": "https://api.github.com/x/4", "size": 2800000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "nightly-build",
    "published_at": "2026-04-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "LiteMonitor-nightly-win-x64.zip", "url": "https://api.github.com/x/5", "size": 2800000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v1.3.4",
    "published_at": "2026-02-12T00:22:24Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "LiteMonitor_v1.3.4-win-x64.zip", "url": "https://api.github.com/x/6", "size": 2800000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：1.3.6/1.3.5 入列表；beta tag 非纯语义丢弃；
// nightly 丢弃；缺 digest 的 1.3.4 丢弃。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]LMRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v1.3.6"]; v.SHA256 != "966c60e0e327d995f8f6edf59c630331a24fdc7aeb4c5765ab539cbf6a6471d7" ||
		v.Size != 2901234 || v.AssetName != "LiteMonitor_v1.3.6-win-x64.zip" {
		t.Errorf("v1.3.6 解析错误: %+v", v)
	}
	for _, gone := range []string{"v1.4.0-beta1", "nightly-build", "v1.3.4"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.Contains(strings.ToLower(r.AssetName), "arm64") {
			t.Errorf("混入非 x64 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：x64 命中，arm64/zip 干扰名绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "LiteMonitor_v1.3.6-win-arm64.zip", Size: 1},
		{Name: "LiteMonitor_v1.3.6-win-x64.msi", Size: 1},
		{Name: "LiteMonitor_v1.3.6-win-x64.zip.sha256", Size: 1},
		{Name: "LiteMonitor_v1.3.6-win-x64.zip", Size: 2901234},
	}
	got, ok := findPortableAsset(assets, "v1.3.6")
	if !ok {
		t.Fatal("应命中 x64 便携资产")
	}
	if got.Name != "LiteMonitor_v1.3.6-win-x64.zip" || got.Size != 2901234 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无便携资产的 release 不命中
	if _, ok := findPortableAsset([]asset{{Name: "LiteMonitor_v1.3.5-win-x64.msi"}}, "v1.3.5"); ok {
		t.Error("仅有 msi 的 release 不应命中")
	}
}

// makeTestZip 构造测试用 zip（保序写入，避免 map 随机序影响断言）
func makeTestZip(t *testing.T, names []string, contents map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "lmtest-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })
	zw := zip.NewWriter(f)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(contents[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// lmZipEntries 官方 zip 真实布局的最小复刻：单层包装目录 + exe + 语言包锚点
// （wrapper 为空串表示平铺布局——剥掉空首段，避免拼出 "/xxx" 绝对路径条目）。
func lmZipEntries(wrapper string) ([]string, map[string]string) {
	p := func(rel ...string) string {
		full := append([]string{wrapper}, rel...)
		return strings.Join(slices.DeleteFunc(full, func(s string) bool { return s == "" }), "/")
	}
	names := []string{p(exeName), p("resources/lang/zh.json"), p("resources/themes/DarkFlat_Classic.json")}
	contents := map[string]string{
		names[0]: "fake-exe",
		names[1]: `{"Menu":{"Exit":"退出"}}`,
		names[2]: `{"name":"DarkFlat_Classic"}`,
	}
	return names, contents
}

// TestVersionFromToken 版本令牌形状：纯 x.y.z 与 imported-时间戳收纳，
// v 前缀/两段/含连字符杂名目录拒绝（与原 dirNameRe 口径一致）。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"1.3.6", "v1.3.6", true},
		{"imported-20260903-120000", "vimported-20260903-120000", true},
		{"v1.3.6", "", false}, // 带 v 前缀的目录名非本模块落位格式
		{"1.3", "", false},    // 必须纯 x.y.z
		{"", "", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK || ver != tt.wantVer {
			t.Errorf("versionFromToken(%q) = (%q,%v), want (%q,%v)", tt.token, ver, ok, tt.wantVer, tt.wantOK)
		}
	}
}

// ---------- 内核解包 + 模块嵌套布局吸收/锚点自检（替代原 extractAll 时代的用例） ----------

func TestResolveStagedRootNestedLayout(t *testing.T) {
	names, contents := lmZipEntries("LiteMonitor_v1.3.6-win-x64")
	zipPath := makeTestZip(t, names, contents)
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	root, err := resolveStagedRoot(staging)
	if err != nil {
		t.Fatalf("resolveStagedRoot: %v", err)
	}
	if filepath.Base(root) != "LiteMonitor_v1.3.6-win-x64" {
		t.Errorf("installRoot 应为包装目录，实际 %s", root)
	}
	if _, err := os.Stat(filepath.Join(root, exeName)); err != nil {
		t.Errorf("%s 未解压: %v", exeName, err)
	}
}

func TestResolveStagedRootFlatLayout(t *testing.T) {
	// 上游若改为平铺发布（无包装目录），同样应被吸收
	names, contents := lmZipEntries("")
	zipPath := makeTestZip(t, names, contents)
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip(平铺): %v", err)
	}
	root, err := resolveStagedRoot(staging)
	if err != nil {
		t.Fatalf("resolveStagedRoot(平铺): %v", err)
	}
	if filepath.Base(root) != "staging" {
		t.Errorf("平铺布局 installRoot 应为暂存根，实际 %s", root)
	}
}

func TestResolveStagedRootRejects(t *testing.T) {
	cases := map[string]struct {
		names    []string
		contents map[string]string
		wantMsg  string
	}{
		"缺 exe": {
			names:    []string{"w/resources/lang/zh.json"},
			contents: map[string]string{"w/resources/lang/zh.json": "{}"},
			wantMsg:  "必须位于根目录或单层包装目录",
		},
		"缺语言包锚点": {
			names:    []string{"w/" + exeName},
			contents: map[string]string{"w/" + exeName: "fake-exe"},
			wantMsg:  "缺少 resources/lang/zh.json",
		},
		"exe 为空": {
			names:    []string{"w/" + exeName, "w/resources/lang/zh.json"},
			contents: map[string]string{"w/" + exeName: "", "w/resources/lang/zh.json": "{}"},
			wantMsg:  "必须位于根目录或单层包装目录",
		},
		"exe 重复": {
			names: []string{"a/" + exeName, "b/" + exeName,
				"b/resources/lang/zh.json"},
			contents: map[string]string{"a/" + exeName: "x", "b/" + exeName: "y",
				"b/resources/lang/zh.json": "{}"},
			wantMsg: "期望唯一的",
		},
		"布局过深": {
			names:    []string{"w/deep/inner/" + exeName, "w/deep/inner/resources/lang/zh.json"},
			contents: map[string]string{"w/deep/inner/" + exeName: "x", "w/deep/inner/resources/lang/zh.json": "{}"},
			wantMsg:  "必须位于根目录或单层包装目录",
		},
	}
	for name, tc := range cases {
		zipPath := makeTestZip(t, tc.names, tc.contents)
		staging := filepath.Join(t.TempDir(), "staging")
		if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
			t.Errorf("%s: UnpackZip 前置失败: %v", name, err)
			continue
		}
		_, err := resolveStagedRoot(staging)
		if err == nil {
			t.Errorf("%s: 应被拒绝", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%s: 错误口径异常: %v", name, err)
		}
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := makeTestZip(t, []string{"../evil.txt"}, map[string]string{"../evil.txt": "escape"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

func TestNormalizeFileVersion(t *testing.T) {
	cases := map[string]string{
		"1.3.6.0":  "1.3.6", // 上游恒发四段（csproj FileVersion）
		"1.3.6":    "1.3.6",
		" 1.3.6.0": "1.3.6",   // 含空白
		"1.3.6.1":  "1.3.6.1", // 非零第四段原样返回交由正则拒绝
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeFileVersion(in); got != want {
			t.Errorf("normalizeFileVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		full := filepath.Join(versionsDir, dir)
		os.MkdirAll(filepath.Join(full, "resources", "lang"), 0755)
		os.WriteFile(filepath.Join(full, exeName), []byte("fake-exe"), 0644)
		os.WriteFile(filepath.Join(full, "resources", "lang", "zh.json"), []byte("{}"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(full, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("litemonitor_imported-20260903-120000", `{"installedAt":"2026-09-03 12:00:00","isImport":true,"source":"D:\\LiteMonitor"}`)
	// 假目录（无 PE 版本资源）：windows 上因核对失败被跳过，非 windows 上会保留
	// （versioninfo 为 windows 构建专属，测试态 fileVersion 是"读真 PE 必失败"的
	// 生产注入）——断言按平台分支；TestDownloadPEVersionMismatchRejects 在 windows
	// 上以注入假 PE 账目核正向拒装路径，两平台合计覆盖核账闸门。
	mkVersion("litemonitor_0.9.9", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755)   // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "litemonitor_1.0.0"), 0755) // 缺 exe 的损坏安装必须跳过
	// 缺语言包锚点的半残目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "litemonitor_2.0.0"), 0755)
	os.WriteFile(filepath.Join(versionsDir, "litemonitor_2.0.0", exeName), []byte("fake"), 0644)
	// 事务前缀目录不得入列表（Tree 扫描忽略 .tmp-/removing；.installing- 直造目录
	// 由 versionFromToken 形状闸拒绝——迁移后防护换形不换口径）
	os.MkdirAll(filepath.Join(versionsDir, "litemonitor_3.0.0.installing-1725350400000000000"), 0755)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	found := map[string]LMVersionInfo{}
	for _, v := range list {
		found[v.Version] = v
	}
	if _, ok := found["litemonitor_ghost"]; ok {
		t.Error("未创建目录不应出现")
	}
	if _, ok := found["v3.0.0"]; ok {
		t.Error(".installing- 事务目录不应出现在列表")
	}
	if v, ok := found["vimported-20260903-120000"]; !ok || !v.IsImport || v.Source != "D:\\LiteMonitor" ||
		v.InstalledAt != "2026-09-03 12:00:00" {
		t.Errorf("imported 兜底目录应可列出并解析元信息: %+v (all: %v)", found, keys(found))
	}
	if _, ok := found["v1.0.0"]; ok {
		t.Error("缺 exe 的损坏目录不应出现")
	}
	if _, ok := found["v2.0.0"]; ok {
		t.Error("缺语言包锚点的半残目录不应出现")
	}
	// 假 exe 无法通过 windows PE 版本核对 → 0.9.9 在 windows 上不可见
	if _, ok := found["v0.9.9"]; ok {
		// 非 windows 平台跳过版本核对，属预期
		if runtimeIsWindows() {
			t.Error("windows 上版本核对失败的假 exe 不应出现")
		}
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("vimported-20260903-120000"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(imported): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove（imported 目录）
	if err := m.Remove("vimported-20260903-120000"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	for _, v := range list {
		if v.Version == "vimported-20260903-120000" {
			t.Error("卸载后不应仍存在")
		}
	}
}

func runtimeIsWindows() bool { return os.PathSeparator == '\\' }

func keys(m map[string]LMVersionInfo) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	// 源：带单层包装目录的解压形态（官方 zip 解出即此布局）
	src := t.TempDir()
	wrapper := filepath.Join(src, "LiteMonitor_v1.3.6-win-x64")
	os.MkdirAll(filepath.Join(wrapper, "resources", "lang"), 0755)
	os.WriteFile(filepath.Join(wrapper, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(wrapper, "resources", "lang", "zh.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(wrapper, "settings.json"), []byte(`{"AutoCheckUpdate":true}`), 0644)
	os.WriteFile(filepath.Join(wrapper, "settings.json.tmp"), []byte("noise"), 0644)
	os.WriteFile(filepath.Join(wrapper, "settings.json.bak"), []byte("noise"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion）
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != wrapper {
		t.Errorf("导入标记错误: %+v", info)
	}
	for _, rel := range []string{exeName, filepath.Join("resources", "lang", "zh.json"), "settings.json"} {
		if _, err := os.Stat(filepath.Join(info.Dir, rel)); err != nil {
			t.Errorf("导入后缺少 %s: %v", rel, err)
		}
	}
	// 上游运行期临时文件绝不带入
	for _, junk := range []string{"settings.json.tmp", "settings.json.bak"} {
		if _, err := os.Stat(filepath.Join(info.Dir, junk)); !os.IsNotExist(err) {
			t.Errorf("临时文件 %s 不应被搬运", junk)
		}
	}
	// 兜底版本可解析、可卸载
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}

	// 源目录完全无 exe → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 exe 的目录应报错")
	}
	// 两个候选包装目录（exe 不唯一）→ 报错
	amb := t.TempDir()
	for _, d := range []string{"one", "two"} {
		os.MkdirAll(filepath.Join(amb, d), 0755)
		os.WriteFile(filepath.Join(amb, d, exeName), []byte("x"), 0644)
	}
	if _, err := m.ImportLocal(amb); err == nil {
		t.Error("exe 不唯一应报错")
	}
}

// TestListingSkipsImportFallbackRecord ImportLocal 的兜底目录名也必须在
// ListInstalled 的扫描半径内（可被列出、可被卸载）——迁移到 Tree 扫描后口径不变。
func TestListingSkipsImportFallbackRecord(t *testing.T) {
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+"imported-20260903-150405")
	os.MkdirAll(filepath.Join(dir, "resources", "lang"), 0755)
	os.WriteFile(filepath.Join(dir, exeName), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(dir, "resources", "lang", "zh.json"), []byte("{}"), 0644)
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

// TestDesktopRuntimeParse 桌面运行时目录枚举（跨平台可测：注入临时目录）。
func TestDesktopRuntimeParse(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"8.0.13", "10.0.2", "8.0.4", "not-a-version", "8.0.13.x.y.z"} {
		os.MkdirAll(filepath.Join(dir, name), 0755)
	}
	os.WriteFile(filepath.Join(dir, "README"), []byte("noise"), 0644)

	vers := desktopRuntimeVersionsUnder(dir)
	if !slices.Contains(vers, "8.0.13") || !slices.Contains(vers, "8.0.4") || !slices.Contains(vers, "10.0.2") {
		t.Fatalf("枚举结果缺失合法版本: %v", vers)
	}
	for _, junk := range vers {
		if junk == "not-a-version" || strings.Count(junk, ".") > 3 {
			t.Errorf("噪声条目混入: %v", vers)
		}
	}
	if !HasDesktopRuntimeMajor(vers, "8") {
		t.Error("应识别 .NET 8 桌面运行时存在")
	}
	if HasDesktopRuntimeMajor([]string{"10.0.2"}, "8") {
		t.Error(".NET 10 不应误判为满足 8.x 需求（不跨大版本回退）")
	}
}
