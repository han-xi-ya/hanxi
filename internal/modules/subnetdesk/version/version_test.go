package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64/aarch64 便携 exe、msi/AppImage 等安装资产、nightly 非规范 tag、
// 缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v1.3.0",
    "published_at": "2026-08-27T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "subnetdesk-1.3.0-aarch64.exe", "url": "https://api.github.com/x/1", "size": 22000000, "digest": "` + h('a') + `"},
      {"name": "subnetdesk-1.3.0-x86_64.msi", "url": "https://api.github.com/x/2", "size": 25000000, "digest": "` + h('b') + `"},
      {"name": "subnetdesk-1.3.0-0.x86_64.rpm", "url": "https://api.github.com/x/3", "size": 29000000, "digest": "` + h('c') + `"},
      {"name": "subnetdesk-1.3.0-x86_64.exe", "url": "https://api.github.com/x/4", "size": 24500000, "digest": "sha256:4983304016aea917fdfc313d5df686735d904b53318bfabcd1270a94cae5b6ce"}
    ]
  },
  {
    "tag_name": "nightly",
    "published_at": "2026-07-17T00:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "subnetdesk-nightly-x86_64.exe", "url": "https://api.github.com/x/5", "size": 24000000, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v1.2.3",
    "published_at": "2026-08-17T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "subnetdesk-1.2.3-x86_64.msi", "url": "https://api.github.com/x/6", "size": 24000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v1.2.2",
    "published_at": "2026-08-09T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "subnetdesk-1.2.2-x86_64.exe", "url": "https://api.github.com/x/7", "size": 24000000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：v1.3.0 入列表（digest 精确剥离）；
// nightly（非规范 tag）、v1.2.3（无便携 exe）、v1.2.2（缺 digest）丢弃。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个版本，实际 %d: %+v", len(list), list)
	}
	r := list[0]
	if r.Version != "v1.3.0" || r.AssetName != "subnetdesk-1.3.0-x86_64.exe" ||
		r.Size != 24500000 || r.IsPre ||
		r.SHA256 != "4983304016aea917fdfc313d5df686735d904b53318bfabcd1270a94cae5b6ce" {
		t.Errorf("v1.3.0 解析错误: %+v", r)
	}
}

// TestFindPortableAsset 资产筛选：x64 命中、aarch64/msi/sciter 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "subnetdesk-1.3.0-aarch64.exe", Size: 1},
		{Name: "subnetdesk-1.3.0-x86_64.msi", Size: 1},
		{Name: "subnetdesk-1.3.0-x86_64-sciter.exe", Size: 1},
		{Name: "subnetdesk-1.3.0-x86_64.exe", Size: 24500000},
	}
	got, ok := findPortableAsset(assets, "v1.3.0")
	if !ok {
		t.Fatal("应命中 x64 便携资产")
	}
	if got.Name != "subnetdesk-1.3.0-x86_64.exe" || got.Size != 24500000 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无便携 exe 的 release 不命中
	if _, ok := findPortableAsset([]asset{{Name: "subnetdesk-1.2.3-x86_64.msi"}}, "v1.2.3"); ok {
		t.Error("仅有 msi 的 release 不应命中")
	}
}

// TestVersionFromImportName 官方资产名版本段提取。
func TestVersionFromImportName(t *testing.T) {
	cases := map[string]string{
		"subnetdesk-1.3.0-x86_64.exe":   "1.3.0",
		"subnetdesk-v1.2.3-aarch64.exe": "1.2.3",
		"subnetdesk-portable.exe":       "",
		"rustdesk-1.4.9-x86_64.exe":     "",
	}
	for name, want := range cases {
		if got := versionFromImportName(name); got != want {
			t.Errorf("versionFromImportName(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestVerifyPEMagic MZ 魔数断言。
func TestVerifyPEMagic(t *testing.T) {
	ok := filepath.Join(t.TempDir(), "a.exe")
	if err := os.WriteFile(ok, []byte("MZ\x90\x00fake"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPEMagic(ok); err != nil {
		t.Errorf("MZ 文件应通过: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "b.exe")
	if err := os.WriteFile(bad, []byte("<html>not found"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPEMagic(bad); err == nil {
		t.Error("非 PE 内容应被拒绝")
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		os.MkdirAll(filepath.Join(versionsDir, dir), 0755)
		os.WriteFile(filepath.Join(versionsDir, dir, exeName), []byte("MZfake-exe"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("subnetdesk_1.3.0", `{"installedAt":"2026-08-28 10:00:00","isImport":true,"source":"E:\\dl"}`)
	mkVersion("subnetdesk_1.2.3", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755)  // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "subnetdesk_1.2.2"), 0755) // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]SDVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v1.3.0"]; !v.IsImport || v.InstalledAt != "2026-08-28 10:00:00" || v.Source != "E:\\dl" {
		t.Errorf("1.3.0 元信息解析错误: %+v", v)
	}
	if v := byVer["v1.2.3"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("1.2.3 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("v1.3.0"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v1.3.0): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("1.3.0"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("v1.3.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	// 目录入参：官方资产命名文件版本段直读，噪声文件不搬运
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "subnetdesk-1.3.0-x86_64.exe"), []byte("MZfake"), 0644)
	os.WriteFile(filepath.Join(src, "subnetdesk-1.3.0-x86_64.msi"), []byte("MZfake"), 0644)
	os.WriteFile(filepath.Join(src, "readme.txt"), []byte("noise"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal(dir): %v", err)
	}
	if info.Version != "v1.3.0" || !info.IsImport {
		t.Errorf("导入结果错误: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, exeName)); err != nil {
		t.Errorf("导入后缺少定名 exe: %v", err)
	}

	// 重复导入拒绝
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("重复导入应报错")
	}

	// 文件入参：用户改名（无版本段）→ 非 PE 内容被魔数断言拦截
	renamed := filepath.Join(t.TempDir(), "my-sd.exe")
	os.WriteFile(renamed, []byte("MZfake"), 0644)
	if _, err := m.ImportLocal(renamed); err != nil {
		// 名字不匹配官方形态 → 拒收（导入语义从严：只收官方资产名）
		if !strings.Contains(err.Error(), "形态") {
			t.Fatalf("改名文件应因形态拒收: %v", err)
		}
	} else {
		t.Error("非官方形态文件名不应导入成功")
	}

	// 合法文件路径直传
	good := filepath.Join(t.TempDir(), "subnetdesk-1.2.3-x86_64.exe")
	os.WriteFile(good, []byte("MZfake"), 0644)
	info2, err := m.ImportLocal(good)
	if err != nil {
		t.Fatalf("ImportLocal(file): %v", err)
	}
	if info2.Version != "v1.2.3" {
		t.Errorf("文件入参版本解析错误: %+v", info2)
	}

	// install.exe 形态（安装器）拒收
	bomb := filepath.Join(t.TempDir(), "subnetdesk-1.1.0-install.exe")
	os.WriteFile(bomb, []byte("MZfake"), 0644)
	if _, err := m.ImportLocal(bomb); err == nil {
		t.Error("install.exe 形态必须拒收")
	}

	// 空目录 / 不存在路径
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无便携 exe 的目录应报错")
	}
	if _, err := m.ImportLocal(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("不存在路径应报错")
	}
}
