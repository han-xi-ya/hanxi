package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 windows msi / macos dmg / linux deb+rpm、预发布 alpha、非规范 tag、
// 缺失 digest 的 release、仅便携 zip 的远古版本。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v2.1.1",
    "published_at": "2026-04-01T13:56:36Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "keyviz_2.1.1_macos.dmg", "url": "https://api.github.com/x/1", "size": 2296295, "digest": "` + h('a') + `"},
      {"name": "keyviz_2.1.1_windows.msi", "url": "https://api.github.com/x/2", "size": 4157440, "digest": "sha256:b58263eb3be44cbba6c5f7518a63bcf7ae31396493dfc1ff143677e7ab710b3a"}
    ]
  },
  {
    "tag_name": "v2.1.0",
    "published_at": "2026-01-26T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "keyviz_2.1.0_macos.dmg", "url": "https://api.github.com/x/3", "size": 4232800, "digest": "` + h('c') + `"},
      {"name": "keyviz_2.1.0_windows.msi", "url": "https://api.github.com/x/4", "size": 4161536, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v2.0.0a3",
    "published_at": "2025-08-03T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "keyviz-v2.0.0a3-windows.zip", "url": "https://api.github.com/x/5", "size": 13083428, "digest": "` + h('e') + `"},
      {"name": "keyviz-v2.0.0a3-linux.deb", "url": "https://api.github.com/x/6", "size": 15614510, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "nightly",
    "published_at": "2026-02-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "keyviz_nightly_windows.msi", "url": "https://api.github.com/x/7", "size": 4000000, "digest": "` + h('0') + `"}
    ]
  },
  {
    "tag_name": "v2.0.5",
    "published_at": "2025-12-01T02:41:30Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "keyviz_2.0.5_windows.msi", "url": "https://api.github.com/x/8", "size": 4100000}
    ]
  },
  {
    "tag_name": "v1.0.6",
    "published_at": "2022-08-28T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "keyviz-v1.0.6-portable.zip", "url": "https://api.github.com/x/9", "size": 14515745},
      {"name": "keyviz-v1.0.6.zip", "url": "https://api.github.com/x/10", "size": 8767576}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：v2.1.1/v2.1.0 入列表；v2.0.0a3 丢弃
// （tag 非纯语义，alpha 预发布系列宁缺毋滥）；nightly 丢弃；v2.0.5 丢弃
// （缺 digest）；v1.0.6 丢弃（远古版本仅便携 zip 无 MSI）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]KeyvizRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v2.1.1"]; v.SHA256 != "b58263eb3be44cbba6c5f7518a63bcf7ae31396493dfc1ff143677e7ab710b3a" ||
		v.Size != 4157440 || v.AssetName != "keyviz_2.1.1_windows.msi" {
		t.Errorf("v2.1.1 解析错误: %+v", v)
	}
	for _, gone := range []string{"v2.0.0a3", "nightly", "v2.0.5", "v1.0.6"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		lower := strings.ToLower(r.AssetName)
		if !strings.HasSuffix(lower, "_windows.msi") {
			t.Errorf("混入非 Windows MSI 资产: %s", r.AssetName)
		}
		if strings.Contains(lower, "dmg") || strings.Contains(lower, "deb") || strings.Contains(lower, "zip") {
			t.Errorf("混入非 MSI 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindMSIAsset 资产筛选：windows msi 精确命中；dmg/deb/zip 绝不混入；无 MSI 不命中。
func TestFindMSIAsset(t *testing.T) {
	assets := []asset{
		{Name: "keyviz_2.1.1_macos.dmg", Size: 1},
		{Name: "keyviz-v2.1.1-linux.deb", Size: 1},
		{Name: "keyviz_2.1.1_windows.msi", Size: 4157440},
	}
	got, ok := findMSIAsset(assets, "v2.1.1")
	if !ok {
		t.Fatal("应命中 Windows MSI 资产")
	}
	if got.Name != "keyviz_2.1.1_windows.msi" || got.Size != 4157440 {
		t.Errorf("命中错误资产: %+v", got)
	}

	if _, ok := findMSIAsset([]asset{{Name: "keyviz_2.1.1_macos.dmg"}}, "v2.1.1"); ok {
		t.Error("仅有 macOS 资产的 release 不应命中")
	}
	// 兜底形状：命名大小写漂移仍可按版本命中
	fuzzy := []asset{{Name: "Keyviz_2.1.1_WINDOWS.MSI", Size: 7}}
	if _, ok := findMSIAsset(fuzzy, "v2.1.1"); !ok {
		t.Error("大小写漂移的兜底匹配应命中")
	}
}

// makeFakeStage 构造与 msiexec /a 管理映像同构的目录树（实测 v2.1.1 布局）：
// PFiles/keyviz/{keyviz.exe, extra/icon.png} + 映像根部的源 msi 副本。
func makeFakeStage(t *testing.T) string {
	t.Helper()
	stage := t.TempDir()
	payload := filepath.Join(stage, "PFiles", "keyviz")
	if err := os.MkdirAll(filepath.Join(payload, "extra"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(payload, filepath.FromSlash(rel)), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(exeName, "fake-exe")
	write("extra/icon.png", "png")
	if err := os.WriteFile(filepath.Join(stage, "keyviz.msi"), []byte("source-copy"), 0644); err != nil {
		t.Fatal(err)
	}
	return stage
}

// TestFindPayloadDir 递归定位 exe 所在目录（大小写不敏感、层级无关）。
func TestFindPayloadDir(t *testing.T) {
	stage := makeFakeStage(t)
	got := findPayloadDir(stage, exeName)
	if want := filepath.Join(stage, "PFiles", "keyviz"); got != want {
		t.Fatalf("findPayloadDir = %q, 期望 %q", got, want)
	}
	if got := findPayloadDir(stage, "KEYVIZ.EXE"); got == "" {
		t.Fatal("大小写不敏感匹配失败")
	}
	empty := t.TempDir()
	if got := findPayloadDir(empty, exeName); got != "" {
		t.Errorf("空目录应返回空串: %q", got)
	}
}

// TestCopyTree 收割布局：payload 内容平铺进目标目录，映像根部的源 msi 副本不被带入。
func TestCopyTree(t *testing.T) {
	stage := makeFakeStage(t)
	dst := filepath.Join(t.TempDir(), "keyviz_2.1.1")
	if err := copyTree(filepath.Join(stage, "PFiles", "keyviz"), dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	for _, rel := range []string{exeName, filepath.Join("extra", "icon.png")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "keyviz.msi")); !os.IsNotExist(err) {
		t.Error("管理映像根部的源 msi 副本不应被收割")
	}
}

// TestExtractMSIGarbageInput extractMSI 对非法 MSI 输入必须报错并清理目标目录。
// 真实成功路径（msiexec 全链路）已在侦查阶段真机验证，单测锁定失败语义。
func TestExtractMSIGarbageInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.msi")
	if err := os.WriteFile(bad, []byte("not a real msi"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "keyviz_9.9.9")
	if err := extractMSI(bad, dst); err == nil {
		t.Fatal("垃圾 MSI 输入应报错")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("失败后目标目录应被清理, stat err=%v", err)
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		os.MkdirAll(filepath.Join(versionsDir, dir), 0755)
		os.WriteFile(filepath.Join(versionsDir, dir, exeName), []byte("fake-exe"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("keyviz_2.1.1", `{"installedAt":"2026-04-02 10:00:00","isImport":true,"source":"C:\\Program Files\\keyviz"}`)
	mkVersion("keyviz_2.1.0", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "keyviz_2.0.9"), 0755)    // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]KeyvizVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v2.1.1"]; !v.IsImport || v.InstalledAt != "2026-04-02 10:00:00" || v.Source != "C:\\Program Files\\keyviz" {
		t.Errorf("2.1.1 元信息解析错误: %+v", v)
	}
	if v := byVer["v2.1.0"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("2.1.0 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("v2.1.1"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v2.1.1): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("2.1.1"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("v2.1.1"); err != nil {
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
	if _, err := os.Stat(filepath.Join(info.Dir, exeName)); err != nil {
		t.Errorf("导入后缺少 %s: %v", exeName, err)
	}
	// 非白名单文件绝不搬运（keyviz 配置在 %APPDATA%\org.keyviz，与 exe 无关）
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
