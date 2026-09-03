package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64/arm64 MSI、NSIS setup、dmg/deb/AppImage、预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v1.4.1",
    "published_at": "2026-08-31T14:24:59Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PicLite_1.4.1_x64-setup.exe", "url": "https://api.github.com/x/1", "size": 5064344, "digest": "` + h('a') + `"},
      {"name": "PicLite_1.4.1_arm64_en-US.msi", "url": "https://api.github.com/x/2", "size": 5758976, "digest": "` + h('b') + `"},
      {"name": "PicLite_1.4.1_x64.dmg", "url": "https://api.github.com/x/3", "size": 7678325, "digest": "` + h('c') + `"},
      {"name": "PicLite_1.4.1_amd64.deb", "url": "https://api.github.com/x/4", "size": 7943712, "digest": "` + h('d') + `"},
      {"name": "PicLite_1.4.1_x64_en-US.msi", "url": "https://api.github.com/x/5", "size": 5943296, "digest": "sha256:46f5fc93d36983a4ef061015db995bb72979bbec982a9b125e37f827e0f32a12"}
    ]
  },
  {
    "tag_name": "v1.4.0",
    "published_at": "2026-08-31T09:40:13Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PicLite_1.4.0_x64_en-US.msi", "url": "https://api.github.com/x/6", "size": 5943296, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v1.3.0-rc1",
    "published_at": "2026-08-30T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "PicLite_1.3.0-rc1_x64_en-US.msi", "url": "https://api.github.com/x/7", "size": 5000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "nightly",
    "published_at": "2026-08-30T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PicLite_nightly_x64_en-US.msi", "url": "https://api.github.com/x/8", "size": 5000000, "digest": "` + h('g') + `"}
    ]
  },
  {
    "tag_name": "v1.2.0",
    "published_at": "2026-08-30T02:41:30Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PicLite_1.2.0_x64-setup.exe", "url": "https://api.github.com/x/9", "size": 5037311},
      {"name": "PicLite_1.2.0_x64_en-US.msi", "url": "https://api.github.com/x/10", "size": 5906432}
    ]
  },
  {
    "tag_name": "v1.1.0",
    "published_at": "2026-08-28T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PicLite_1.1.0_aarch64.dmg", "url": "https://api.github.com/x/11", "size": 7000000, "digest": "` + h('i') + `"}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：v1.4.1/v1.4.0 入列表；
// v1.3.0-rc1 丢弃（tag 非纯语义，与 ccswitch 策略一致宁缺毋滥）；nightly 丢弃；
// v1.2.0 丢弃（缺 digest）；v1.1.0 丢弃（无 Windows 资产）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]PicRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v1.4.1"]; v.SHA256 != "46f5fc93d36983a4ef061015db995bb72979bbec982a9b125e37f827e0f32a12" ||
		v.Size != 5943296 || v.AssetName != "PicLite_1.4.1_x64_en-US.msi" {
		t.Errorf("v1.4.1 解析错误: %+v", v)
	}
	for _, gone := range []string{"v1.3.0-rc1", "nightly", "v1.2.0", "v1.1.0"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		lower := strings.ToLower(r.AssetName)
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "setup.exe") ||
			strings.Contains(lower, "dmg") || strings.Contains(lower, "deb") {
			t.Errorf("混入非 x64 MSI 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindMSIAsset 资产筛选：x64 精确命中；arm64/setup/dmg 绝不混入；无 MSI 不命中。
func TestFindMSIAsset(t *testing.T) {
	assets := []asset{
		{Name: "PicLite_1.4.1_x64-setup.exe", Size: 1},
		{Name: "PicLite_1.4.1_arm64_en-US.msi", Size: 1},
		{Name: "PicLite_1.4.1_x64_en-US.msi", Size: 5943296},
	}
	got, ok := findMSIAsset(assets, "v1.4.1")
	if !ok {
		t.Fatal("应命中 x64 MSI 资产")
	}
	if got.Name != "PicLite_1.4.1_x64_en-US.msi" || got.Size != 5943296 {
		t.Errorf("命中错误资产: %+v", got)
	}

	if _, ok := findMSIAsset([]asset{{Name: "PicLite_1.4.1_x64-setup.exe"}}, "v1.4.1"); ok {
		t.Error("仅有 NSIS setup 的 release 不应命中")
	}
	// 兜底形状：本地化名轻微漂移（大小写/区域后缀）仍可按版本命中
	fuzzy := []asset{{Name: "PicLite_1.4.1_x64_EN-US.MSI", Size: 7}}
	if _, ok := findMSIAsset(fuzzy, "v1.4.1"); !ok {
		t.Error("大小写漂移的兜底匹配应命中")
	}
}

// makeFakeStage 构造与 msiexec /a 管理映像同构的目录树：
// PFiles/PicLite/{piclite.exe, extra/icon.png} + 映像根部的源 msi 副本。
func makeFakeStage(t *testing.T) string {
	t.Helper()
	stage := t.TempDir()
	payload := filepath.Join(stage, "PFiles", "PicLite")
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
	if err := os.WriteFile(filepath.Join(stage, "piclite.msi"), []byte("source-copy"), 0644); err != nil {
		t.Fatal(err)
	}
	return stage
}

// TestFindPayloadDir 递归定位 exe 所在目录（大小写不敏感、层级无关）。
func TestFindPayloadDir(t *testing.T) {
	stage := makeFakeStage(t)
	got := findPayloadDir(stage, exeName)
	if want := filepath.Join(stage, "PFiles", "PicLite"); got != want {
		t.Fatalf("findPayloadDir = %q, 期望 %q", got, want)
	}
	if got := findPayloadDir(stage, "PICLITE.EXE"); got == "" {
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
	dst := filepath.Join(t.TempDir(), "piclite_1.4.1")
	if err := copyTree(filepath.Join(stage, "PFiles", "PicLite"), dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	for _, rel := range []string{exeName, filepath.Join("extra", "icon.png")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "piclite.msi")); !os.IsNotExist(err) {
		t.Error("管理映像根部的源 msi 副本不应被收割")
	}
}

// TestExtractMSIGarbageInput extractMSI 对非法 MSI 输入必须报错并清理目标目录。
// 真实成功路径（msiexec 全链路）在真机联调阶段验证，此处只锁定失败语义。
func TestExtractMSIGarbageInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.msi")
	if err := os.WriteFile(bad, []byte("not a real msi"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "piclite_9.9.9")
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
	mkVersion("piclite_1.4.1", `{"installedAt":"2026-08-31 10:00:00","isImport":true,"source":"E:\\piclite"}`)
	mkVersion("piclite_1.4.0", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "piclite_1.3.1"), 0755)   // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]PicVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v1.4.1"]; !v.IsImport || v.InstalledAt != "2026-08-31 10:00:00" || v.Source != "E:\\piclite" {
		t.Errorf("1.4.1 元信息解析错误: %+v", v)
	}
	if v := byVer["v1.4.0"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("1.4.0 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("v1.4.1"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v1.4.1): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("1.4.1"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("v1.4.1"); err != nil {
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
	// 非白名单文件绝不搬运
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
