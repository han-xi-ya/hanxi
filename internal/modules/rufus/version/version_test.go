package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应（资产布局照 v4.15 实测）：
// 覆盖便携 p.exe、安装版、x86/arm64 变体、.sig、beta tag、缺 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v4.15",
    "published_at": "2026-06-30T11:31:20Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "rufus-4.15.exe", "url": "https://api.github.com/x/1", "size": 1989992, "digest": "` + h('a') + `"},
      {"name": "rufus-4.15.exe.sig", "url": "https://api.github.com/x/2", "size": 256, "digest": "` + h('b') + `"},
      {"name": "rufus-4.15p.exe", "url": "https://api.github.com/x/3", "size": 1989992, "digest": "sha256:84c8a437f8af89257524478489e5c85f1edf25f761d299e2bcde46ac0afbe106"},
      {"name": "rufus-4.15_arm64.exe", "url": "https://api.github.com/x/4", "size": 5491048, "digest": "` + h('c') + `"},
      {"name": "rufus-4.15_x86.exe", "url": "https://api.github.com/x/5", "size": 1933672, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v4.14",
    "published_at": "2026-04-30T11:32:59Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "rufus-4.14p.exe", "url": "https://api.github.com/x/6", "size": 2002280, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v4.5b1",
    "published_at": "2024-08-26T13:23:17Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "rufus-4.5b1p.exe", "url": "https://api.github.com/x/7", "size": 1900000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v4.13",
    "published_at": "2026-02-17T21:05:19Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "rufus-4.13p.exe", "url": "https://api.github.com/x/8", "size": 1946984}
    ]
  },
  {
    "tag_name": "v4.12",
    "published_at": "2026-01-30T13:39:39Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "rufus-4.12_x86.exe", "url": "https://api.github.com/x/9", "size": 1892712, "digest": "` + h('0') + `"}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：4.15/4.14 入列表；beta tag 丢弃；
// 缺 digest 的 4.13 丢弃；只有架构变体的 4.12 丢弃。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]RufusRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v4.15"]; v.SHA256 != "84c8a437f8af89257524478489e5c85f1edf25f761d299e2bcde46ac0afbe106" ||
		v.Size != 1989992 || v.AssetName != "rufus-4.15p.exe" {
		t.Errorf("v4.15 解析错误: %+v", v)
	}
	for _, gone := range []string{"v4.5b1", "v4.13", "v4.12"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		lower := strings.ToLower(r.AssetName)
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "_x86") || strings.HasSuffix(lower, ".sig") {
			t.Errorf("混入非 x64 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：精确命中 p.exe；p.exe 缺失时退到同哈希安装版；
// 变体与 sig 绝不混入；皆无则不命中。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "rufus-4.15.exe.sig", Size: 256},
		{Name: "rufus-4.15_arm64.exe", Size: 5},
		{Name: "rufus-4.15_x86.exe", Size: 5},
		{Name: "rufus-4.15.exe", Size: 1989992},
		{Name: "rufus-4.15p.exe", Size: 1989992},
	}
	got, ok := findPortableAsset(assets, "v4.15")
	if !ok || got.Name != "rufus-4.15p.exe" {
		t.Fatalf("应命中 rufus-4.15p.exe，实际 %+v ok=%v", got, ok)
	}

	// 无 p.exe：退到安装版精确名（实证两形态字节级同哈希）
	onlyInstaller := []asset{{Name: "rufus-4.16.exe", Size: 1}, {Name: "rufus-4.16_arm64.exe", Size: 1}}
	if got, ok := findPortableAsset(onlyInstaller, "v4.16"); !ok || got.Name != "rufus-4.16.exe" {
		t.Errorf("缺 p.exe 时应退到安装版精确名: %+v ok=%v", got, ok)
	}

	// 只有架构变体/sig → 不命中
	if _, ok := findPortableAsset([]asset{{Name: "rufus-4.14_x86.exe"}, {Name: "rufus-4.14.exe.sig"}}, "v4.14"); ok {
		t.Error("仅变体的 release 不应命中")
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) string {
		full := filepath.Join(versionsDir, dir)
		if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(full, exeName), []byte("MZfake-exe"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(full, "meta.json"), []byte(meta), 0644)
		}
		return full
	}
	mkVersion("rufus_4.15", `{"installedAt":"2026-07-01 09:00:00"}`)
	mkVersion("rufus_4.9", "")
	mkVersion("rufus_imported-20260903-120000", `{"installedAt":"2026-09-03 12:00:00","isImport":true,"source":"D:\\Downloads"}`)
	mkVersion("rufus_4.15.removing-123", "")                         // Remove 中途失败的残骸必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "rufus_1.0"), 0755)       // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	found := map[string]RufusVersionInfo{}
	for _, v := range list {
		found[v.Version] = v
	}
	if v, ok := found["v4.15"]; !ok || v.InstalledAt != "2026-07-01 09:00:00" {
		t.Errorf("v4.15 未列出或元信息错误: %+v", found)
	}
	if _, ok := found["v4.9"]; !ok {
		t.Error("v4.9 应列出")
	}
	if v, ok := found["vimported-20260903-120000"]; !ok || !v.IsImport || v.Source != "D:\\Downloads" {
		t.Errorf("imported 兜底目录应可列出并解析元信息: %+v", found)
	}
	if _, ok := found["v1.0"]; ok {
		t.Error("缺 exe 的损坏目录不应出现")
	}
	for _, junk := range []string{"v4.15.removing-123", "ccswitch_3.20.0"} {
		if _, ok := found[junk]; ok {
			t.Errorf("噪声目录混入: %s", junk)
		}
	}
	// 排序：数值分段降序（v4.15 > v4.9），imported- 兜底目录恒沉底
	wantOrder := []string{"v4.15", "v4.9", "vimported-20260903-120000"}
	if len(list) != len(wantOrder) {
		t.Fatalf("列表长度错误: %+v", list)
	}
	for i, want := range wantOrder {
		if list[i].Version != want {
			t.Errorf("排序错误 [%d] = %s, want %s（全部: %+v）", i, list[i].Version, want, list)
			break
		}
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("v4.15"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe: %v %v", exe, err)
	}
	if _, err := m.ResolveExe("v9.9"); err == nil {
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

// TestImportLocal 导入：文件形态（官方 p.exe 命名 + 随行 rufus.ini 搬运）、
// 目录形态（多候选取版本最大）、形态外拒收。
func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	// 文件形态：浏览器下载原名 rufus-4.13p.exe + 便携配置随行
	srcDir := t.TempDir()
	srcExe := filepath.Join(srcDir, "rufus-4.13p.exe")
	os.WriteFile(srcExe, []byte("MZfake"), 0644)
	os.WriteFile(filepath.Join(srcDir, iniFileName), []byte("Locale = zh-CN"), 0644)
	info, err := m.ImportLocal(srcExe)
	if err != nil {
		t.Fatalf("ImportLocal(file): %v", err)
	}
	if info.Version != "v4.13" || !info.IsImport || info.Source != srcDir {
		t.Errorf("导入结果错误: %+v", info)
	}
	iniData, err := os.ReadFile(filepath.Join(info.Dir, iniFileName))
	if err != nil || string(iniData) != "Locale = zh-CN" {
		t.Errorf("随行 rufus.ini 未搬运: %v %q", err, iniData)
	}
	if _, err := m.ResolveExe("v4.13"); err != nil {
		t.Errorf("导入版本应可解析: %v", err)
	}
	// 重复导入同名版本 → 拒绝
	if _, err := m.ImportLocal(srcExe); err == nil {
		t.Error("已安装版本重复导入应报错")
	}

	// 目录形态：多候选取版本最大（4.9 字典序大于 4.15，必须数值比较）
	dir := t.TempDir()
	for _, n := range []string{"rufus-4.15p.exe", "rufus-4.9p.exe", "rufus-4.15_x86.exe", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("MZx"), 0644)
	}
	info, err = m.ImportLocal(dir)
	if err != nil {
		t.Fatalf("ImportLocal(dir): %v", err)
	}
	if info.Version != "v4.15" {
		t.Errorf("目录导入应取版本最大候选，实际 %q", info.Version)
	}

	// 形态外文件名拒收（自造命名 / 非 exe）
	odd := filepath.Join(t.TempDir(), "rufus-setup.exe")
	os.WriteFile(odd, []byte("MZx"), 0644)
	if _, err := m.ImportLocal(odd); err == nil {
		t.Error("非官方形态文件名应拒收")
	}
	if _, err := m.ImportLocal(filepath.Join(dir, "notes.txt")); err == nil {
		t.Error("非 exe 应拒收")
	}
	// 用户改名（rufus.exe）：假 PE 读不到版本 → 时间戳兜底可安装
	renamed := filepath.Join(t.TempDir(), exeName)
	os.WriteFile(renamed, []byte("MZx"), 0644)
	info, err = m.ImportLocal(renamed)
	if err != nil {
		t.Fatalf("ImportLocal(rufus.exe): %v", err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("改名导入应走时间戳兜底，实际 %q", info.Version)
	}
}

func TestVersionHelpers(t *testing.T) {
	cases := map[string]string{
		"rufus-4.15p.exe": "4.15",
		"rufus-4.9.exe":   "4.9",
		"Rufus-4.15P.EXE": "4.15",
		"rufus.exe":       "",
		"rufus-combo.exe": "",
	}
	for in, want := range cases {
		if got := versionFromImportName(in); got != want {
			t.Errorf("versionFromImportName(%q) = %q, want %q", in, got, want)
		}
	}
	seg := map[string]string{
		"4.15.1989": "4.15", // 上游 FileVersion 含构建号
		"4.9.0.0":   "4.9",
		"4.15":      "4.15",
		" 4.15.2 ":  "4.15",
		"garbage.x": "garbage.x", // 非数字交由 plainVersionRe 拒绝
		"single":    "single",
	}
	for in, want := range seg {
		if got := firstTwoSegments(in); got != want {
			t.Errorf("firstTwoSegments(%q) = %q, want %q", in, got, want)
		}
	}
}
