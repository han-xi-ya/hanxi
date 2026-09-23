package version

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
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

// ---------- 测试夹具：假管理映像与接缝注入 ----------

// makeFakeStage 构造与 msiexec /a 管理映像同构的目录树（实测 v2.1.1 布局）：
// PFiles/keyviz/{keyviz.exe, extra/icon.png} + 映像根部的源 msi 副本。
func makeFakeStage(t *testing.T) string {
	t.Helper()
	stage := t.TempDir()
	writeFakeAdminImage(t, stage)
	return stage
}

// writeFakeAdminImage 在给定目录落一份假管理映像布局（假 msiExtract 使用）。
func writeFakeAdminImage(t *testing.T, stage string) {
	t.Helper()
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
}

// withFakeMSIExtract 注入假 msiExtract 接缝，测试结束复位（本包测试串行、无 t.Parallel）。
func withFakeMSIExtract(t *testing.T, fn func(msiPath, stage string) error) {
	t.Helper()
	old := msiExtract
	msiExtract = fn
	t.Cleanup(func() { msiExtract = old })
}

// compressMSIPollWindow 压缩管理提取落盘防御轮询窗口（缺失 payload 用例免等 10s）。
func compressMSIPollWindow(t *testing.T) {
	t.Helper()
	oldB, oldI := msiPayloadWaitBudget, msiPayloadPollInterval
	msiPayloadWaitBudget, msiPayloadPollInterval = 300*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { msiPayloadWaitBudget, msiPayloadPollInterval = oldB, oldI })
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
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
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

// TestVersionFromToken 版本令牌形状：纯 x.y.z 与 imported-时间戳收纳，
// v 前缀/两段/中文目录名拒绝（与原 dirNameRe 口径一致）。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"2.1.1", "v2.1.1", true},
		{"imported-20260918-150405", "vimported-20260918-150405", true},
		{"v2.1.1", "", false}, // 带 v 前缀的目录名非本模块落位格式
		{"2.1", "", false},    // 必须纯 x.y.z
		{"", "", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK || ver != tt.wantVer {
			t.Errorf("versionFromToken(%q) = (%q,%v), want (%q,%v)", tt.token, ver, ok, tt.wantVer, tt.wantOK)
		}
	}
}

// ---------- msiexec 命令构造（注入防护钉死） ----------

// TestBuildMSIExtractCmdFixedArgv 受控常量拼接回归钉：argv 恒为定形五元组，
// TARGETDIR 以单一 "TARGETDIR=<stage>" argv 元素传递（不经 shell、无插值面）。
// 生产真实执行路径另由 TestExtractMSIGarbageInput（真 msiexec）与侦查阶段
// 真机验证覆盖；此处在非 Windows 也能钉住形状，防重构漂移。
func TestBuildMSIExtractCmdFixedArgv(t *testing.T) {
	msi := filepath.Join(t.TempDir(), "hanxi-keyviz-x.msi")
	if err := os.WriteFile(msi, []byte("msi"), 0644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "admin-image")
	cmd, err := buildMSIExtractCmd(msi, stage)
	if err != nil {
		t.Fatalf("合法路径应通过: %v", err)
	}
	want := []string{"msiexec.exe", "/a", msi, "/qn", "TARGETDIR=" + stage}
	if len(cmd.Args) != len(want) {
		t.Fatalf("argv 形状漂移: %q", cmd.Args)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
	// 不断言 cmd.Err（msiexec.exe 是否可在 PATH 解析属环境事实、非 argv 形状契约；
	// 受限会话 PATH 精简会假红,同 TROUBLESHOOTING #79/#81 家族）。真实提取链由
	// nsis 冒烟在 SystemRoot 兜底解析处覆盖,本例只钉形受控 argv 五元组。
}

// TestBuildMSIExtractCmdRejectsHostilePaths 纵深防御：空/相对/含引号与控制字符
// 的路径一律拒进命令行（含 TARGETDIR——消毒口径为"只接受绝对、无歧义字符路径"）。
func TestBuildMSIExtractCmdRejectsHostilePaths(t *testing.T) {
	base := t.TempDir()
	goodMSI := filepath.Join(base, "x.msi")
	cases := []struct{ name, msi, stage string }{
		{"空 MSI 路径", "", filepath.Join(base, "stage")},
		{"空 TARGETDIR", goodMSI, ""},
		{"相对 MSI", "x.msi", filepath.Join(base, "stage")},
		{"相对 TARGETDIR", goodMSI, "stage"},
		{"TARGETDIR 含引号", goodMSI, filepath.Join(base, `evil"dir`)},
		{"TARGETDIR 含换行", goodMSI, filepath.Join(base, "evil\ndir")},
		{"MSI 含回车", filepath.Join(base, "evil\r.msi"), filepath.Join(base, "stage")},
	}
	for _, c := range cases {
		if _, err := buildMSIExtractCmd(c.msi, c.stage); err == nil {
			t.Errorf("%s 应被拒绝", c.name)
		}
	}
}

// ---------- extractMSI（假 msiExtract 接缝驱动） ----------

// TestExtractMSIFakeRunnerSuccess 成功链：假管理安装落盘 PFiles 布局 →
// 收割平铺进 staging，映像根部源 msi 副本不入 staging，布局自检通过。
func TestExtractMSIFakeRunnerSuccess(t *testing.T) {
	var gotMSI, gotStage string
	withFakeMSIExtract(t, func(msiPath, stage string) error {
		gotMSI, gotStage = msiPath, stage
		writeFakeAdminImage(t, stage)
		return nil
	})
	msi := filepath.Join(t.TempDir(), "keyviz_2.1.1_windows.msi")
	if err := os.WriteFile(msi, []byte("msi-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	if err := extractMSI(context.Background(), msi, staging); err != nil {
		t.Fatalf("extractMSI: %v", err)
	}
	if gotMSI != msi {
		t.Errorf("接缝应收到 MSI 缓存路径: %q", gotMSI)
	}
	if !filepath.IsAbs(gotStage) || gotStage == staging || strings.Contains(gotStage, "keyviz_") {
		t.Errorf("TARGETDIR 应为独立绝对管理映像目录（非 staging、非版本目录）: %q", gotStage)
	}
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.Size() == 0 {
		t.Fatalf("收割后 staging 应有可用 %s: %v", exeName, err)
	}
	if _, err := os.Stat(filepath.Join(staging, "extra", "icon.png")); err != nil {
		t.Errorf("payload 子目录应保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "keyviz.msi")); !os.IsNotExist(err) {
		t.Error("映像根部源 msi 副本不得进入 staging")
	}
}

// TestExtractMSIRunnerErrorPropagates 管理安装失败 → 错误透传（staging 由调用方 discard）。
func TestExtractMSIRunnerErrorPropagates(t *testing.T) {
	withFakeMSIExtract(t, func(string, string) error {
		return &fakeExtractError{}
	})
	staging := t.TempDir()
	if err := extractMSI(context.Background(), filepath.Join(t.TempDir(), "x.msi"), staging); err == nil {
		t.Fatal("接缝报错应透传")
	}
	ents, _ := os.ReadDir(staging)
	if len(ents) != 0 {
		t.Errorf("失败路径不得向 staging 搬运任何内容: %v", ents)
	}
}

type fakeExtractError struct{}

func (*fakeExtractError) Error() string { return "msiexec 管理提取失败: fake" }

// TestExtractMSIMissingPayload 管理安装"成功"但映像无 keyviz.exe →
// 轮询预算耗尽后报"管理提取无效"，且 staging 不被污染。
func TestExtractMSIMissingPayload(t *testing.T) {
	compressMSIPollWindow(t)
	withFakeMSIExtract(t, func(_, stage string) error { return nil }) // 空映像
	staging := t.TempDir()
	err := extractMSI(context.Background(), filepath.Join(t.TempDir(), "x.msi"), staging)
	if err == nil || !strings.Contains(err.Error(), "管理提取无效") {
		t.Fatalf("空映像应报管理提取无效, got %v", err)
	}
	ents, _ := os.ReadDir(staging)
	if len(ents) != 0 {
		t.Errorf("失败路径不得向 staging 搬运任何内容: %v", ents)
	}
}

// TestExtractMSIEmptyExeRejected 布局自检：payload 提取成功但 exe 为 0 字节 → 判无效。
func TestExtractMSIEmptyExeRejected(t *testing.T) {
	withFakeMSIExtract(t, func(_, stage string) error {
		payload := filepath.Join(stage, "PFiles", "keyviz")
		if err := os.MkdirAll(payload, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(payload, exeName), nil, 0644)
	})
	if err := extractMSI(context.Background(), filepath.Join(t.TempDir(), "x.msi"), t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "MSI 布局无效") {
		t.Fatalf("空 exe 应判布局无效, got %v", err)
	}
}

// TestExtractMSIGarbageInput 真实 msiexec 接缝默认实现对非法 MSI 输入必须报错
// （msiexec 返回非零码；非 Windows 平台 exec 找不到 msiexec.exe 同样报错）。
// 真实成功路径（msiexec 全链路）已在侦查阶段真机验证，单测锁定失败语义。
func TestExtractMSIGarbageInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.msi")
	if err := os.WriteFile(bad, []byte("not a real msi"), 0644); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	if err := extractMSI(context.Background(), bad, staging); err == nil {
		t.Fatal("垃圾 MSI 输入应报错")
	}
	if ents, _ := os.ReadDir(staging); len(ents) != 0 {
		t.Errorf("失败后 staging 不得有半件: %v", ents)
	}
}

// ---------- 版本目录账目 ----------

// TestListInstalledAndRemove 扫描/账本双形态（内核 meta + 迁移前 map 账本）、
// 形状过滤、ResolveExe 白名单与 Remove。
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
	// 内核统一账本（新下载链形态，含 schema）：installedAt 取账本、非导入
	kernelMeta, _ := json.Marshal(artifact.Meta{
		Schema: artifact.DefaultSchema, Tool: "keyviz", Version: "2.0.9",
		Entry: exeName, ZipSHA256: strings.Repeat("a", 64), Source: artifact.SourceRemote,
		InstalledAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.Local),
	})
	mkVersion("keyviz_2.0.9", string(kernelMeta))
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "keyviz_1.9.9"), 0755)    // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]KeyvizVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v2.1.1"]; !v.IsImport || v.InstalledAt != "2026-04-02 10:00:00" || v.Source != "C:\\Program Files\\keyviz" {
		t.Errorf("2.1.1 导入账本解析错误: %+v", v)
	}
	if v := byVer["v2.1.0"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("2.1.0 默认元信息错误: %+v", v)
	}
	if v := byVer["v2.0.9"]; v.IsImport || v.InstalledAt != "2026-03-01 08:00:00" || v.Source != "" {
		t.Errorf("2.0.9 内核账本解析错误: %+v", v)
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

	// Remove（Tree：rename 隔离后删除）
	if err := m.Remove("v2.1.1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 2 {
		t.Errorf("卸载后应剩 2 个版本，实际 %d", len(list))
	}
}

// TestImportLocal 导入链（无内核对应物）：白名单搬运、时间戳兜底、重复拒绝。
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
	// 导入目录必须被 Tree 扫描半径收纳（可列出、可卸载）
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || !list[0].IsImport {
		t.Fatalf("导入目录应可列出: %+v %v", list, err)
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
