package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 6.2.0（portable）、6.1.0.1（portable）、6.0.0（portable-x64）、
// framework-dependent zip、setup.exe、预发布、tag 与资产版本不一致、
// 缺 digest 的旧资产。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v6.2",
    "published_at": "2026-06-09T21:57:07Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_6.2.0_net8.0-windows10.0.18362.0.zip", "url": "https://api.github.com/x/1", "size": 12000000, "digest": "` + h('a') + `"},
      {"name": "BCUninstaller_6.2.0_portable.zip", "url": "https://api.github.com/x/2", "size": 76000000, "digest": "sha256:93f6c3543fdff7291efd6d12e33a46cf9f6dd1c91b9e9a53b45e47ee2b7c0010"},
      {"name": "BCUninstaller_6.2.0_setup.exe", "url": "https://api.github.com/x/3", "size": 9000000, "digest": "` + h('b') + `"}
    ]
  },
  {
    "tag_name": "v6.1",
    "published_at": "2026-03-06T00:05:46Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_6.1.0.1_portable.zip", "url": "https://api.github.com/x/4", "size": 76000000, "digest": "` + h('c') + `"},
      {"name": "BCUninstaller_6.1.0.1_setup.exe", "url": "https://api.github.com/x/5", "size": 9000000, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v6.0",
    "published_at": "2026-03-03T08:11:02Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_6.0.0_net8.0-windows10.0.18362.0.zip", "url": "https://api.github.com/x/6", "size": 10000000, "digest": "` + h('e') + `"},
      {"name": "BCUninstaller_6.0.0_portable-x64.zip", "url": "https://api.github.com/x/7", "size": 76000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v5.9",
    "published_at": "2025-07-01T22:18:42Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_5.9.0_portable.zip", "url": "https://api.github.com/x/8", "size": 142000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v5.8",
    "published_at": "2025-02-20T21:28:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_5.8.3_portable.zip", "url": "https://api.github.com/x/9", "size": 141000000}
    ]
  },
  {
    "tag_name": "v7.7",
    "published_at": "2026-07-01T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "BCUninstaller_9.9.0_portable.zip", "url": "https://api.github.com/x/10", "size": 76000000, "digest": "` + h('e') + `"}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：
// 6.2.0 双变体齐备（portable+fdd）；6.1.0.1 仅 portable；6.0.0 portable-x64+fdd；
// version tag 与资产版本不一致的 v9.9 丢弃；缺 digest 的 v5.8 丢弃；v5.9 预发布保留。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("期望 4 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]BCURelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["6.2.0"]; v.Tag != "v6.2" || v.SHA256 != "93f6c3543fdff7291efd6d12e33a46cf9f6dd1c91b9e9a53b45e47ee2b7c0010" ||
		v.Size != 76000000 || v.IsPre {
		t.Errorf("6.2.0 解析错误: %+v", v)
	}
	if v := byVer["6.2.0"]; v.FddName != "BCUninstaller_6.2.0_net8.0-windows10.0.18362.0.zip" ||
		v.FddSize != 12000000 || len(v.FddSHA256) != 64 {
		t.Errorf("6.2.0 fdd 变体解析错误: %+v", v)
	}
	if v := byVer["6.1.0.1"]; v.Tag != "v6.1" {
		t.Errorf("6.1.0.1 解析错误: %+v", v)
	}
	if v := byVer["6.1.0.1"]; v.FddName != "" {
		t.Errorf("6.1.0.1 不应有 fdd 变体（样例未提供）: %+v", v)
	}
	if v := byVer["6.0.0"]; !strings.Contains(v.AssetName, "portable-x64") || v.FddName == "" {
		t.Errorf("6.0.0 便携资产/fdd 变体错误: %+v", v)
	}
	if v, ok := byVer["5.9.0"]; !ok || !v.IsPre {
		t.Errorf("预发布版本应保留并标记 IsPre: %+v", v)
	}
	for _, gone := range []string{"5.8.3", "9.9.0"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.Contains(r.AssetName, "setup") || strings.Contains(r.AssetName, "net") {
			t.Errorf("主资产混入非自包含便携条目: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestParseReleasesFDDFilter fdd 变体过滤：
//  1. fdd 资产 digest 缺失 → 变体不可用但主资产仍入列表；
//  2. fdd 资产版本与 tag 不一致 → 不吸附为变体。
func TestParseReleasesFDDFilter(t *testing.T) {
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {"tag_name": "v6.2", "prerelease": false, "draft": false, "assets": [
    {"name": "BCUninstaller_6.2.0_portable.zip", "url": "u1", "size": 1, "digest": "` + h('a') + `"},
    {"name": "BCUninstaller_6.2.0_net8.0-windows10.0.18362.0.zip", "url": "u2", "size": 2}
  ]},
  {"tag_name": "v6.1", "prerelease": false, "draft": false, "assets": [
    {"name": "BCUninstaller_6.1.0.1_portable.zip", "url": "u3", "size": 3, "digest": "` + h('b') + `"},
    {"name": "BCUninstaller_9.9.0_net8.0-windows10.0.18362.0.zip", "url": "u4", "size": 4, "digest": "` + h('c') + `"}
  ]}
]`
	list, err := parseReleasesBody([]byte(body))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d", len(list))
	}
	if v := list[0]; v.Version != "6.2.0" || v.FddName != "" {
		t.Errorf("缺 digest 的 fdd 不应收录: %+v", v)
	}
	if v := list[1]; v.Version != "6.1.0.1" || v.FddName != "" {
		t.Errorf("tag 不一致的 fdd 不应吸附: %+v", v)
	}
}

// TestDotnetVersions 目录枚举：合法版本收录、噪声目录忽略。
func TestDotnetVersions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"8.0.13", "9.0.1", "6.0.36-blah", "not-a-version", "8.0"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	vers := desktopRuntimeVersionsUnder(dir)
	want := []string{"8.0", "8.0.13", "9.0.1"}
	if len(vers) != len(want) {
		t.Fatalf("版本枚举 = %v, 期望 %v", vers, want)
	}
	for i := range want {
		if vers[i] != want[i] {
			t.Fatalf("版本枚举排序错误: %v", vers)
		}
	}
	if !HasDesktopRuntimeMajor(vers, "8") {
		t.Error("装有 8.0.13 应判定 hasNet8")
	}
	if HasDesktopRuntimeMajor(vers, "7") {
		t.Error("未装 7.x 不应误判")
	}
	// 目录不存在 = 未安装
	if got := desktopRuntimeVersionsUnder(filepath.Join(dir, "nope")); got != nil {
		t.Errorf("不存在目录应返回 nil, 实际 %v", got)
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "bcutest-*.zip")
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

// TestVersionFromToken 版本令牌形状（原 dirNameRe 收纳口径）：
// 3~4 段数字与数字起头的点/字母数字串、imported-时间戳收纳；
// v 前缀/字母起头/中文目录名拒绝（不列入）。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token  string
		wantOK bool
	}{
		{"6.2.0", true},
		{"6.1.0.1", true},
		{"imported-20260827-100000", true},
		{"v6.2.0", false},        // BCU 惯例无 v 前缀
		{"ccswitch-3.20", false}, // 非数字起头
		{"", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK {
			t.Errorf("versionFromToken(%q) ok = %v, want %v", tt.token, ok, tt.wantOK)
		}
		if ok && ver != tt.token {
			t.Errorf("versionFromToken(%q) = %q, BCU 展示口径应原样无 v 前缀", tt.token, ver)
		}
	}
}

// ---------- 内核解包 + 模块布局自检（替代原 extractAll 时代的用例） ----------

func TestUnpackWithBCULayout(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		exeName:                "bootstrapper", // 外层接力启动器
		"win-x64/" + exeName:   "real-app",     // 内层真身（锚点）
		"win-x64/bulkcrap.dll": "fake-dll",
		"LICENSE":              "Apache-2.0",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkLayout(staging); err != nil {
		t.Fatalf("checkLayout: %v", err)
	}
	for _, name := range []string{exeName, "win-x64/" + exeName, "win-x64/bulkcrap.dll", "LICENSE"} {
		if _, err := os.Stat(filepath.Join(staging, filepath.FromSlash(name))); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
	// 账本锚点必须落在内层真身（外层 bootstrapper 不进托管生命周期视野）
	if got := entryRel(staging); got != innerExeRel {
		t.Errorf("entryRel 应指内层真身: got %q want %q", got, innerExeRel)
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		"../evil.txt": "escape",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

func TestLayoutMissingOuterExe(t *testing.T) {
	// 仅有 win-x64 子层、根目录无 BCUninstaller.exe：不符官方便携包布局，拒装
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, "win-x64"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "win-x64", exeName), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}
	err := checkLayout(staging)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("缺外层启动器应自检失败, got %v", err)
	}
}

func TestEntryRelFallsBackToOuter(t *testing.T) {
	// 老布局（fdd 精简包等无 win-x64 子层）：账本锚点回退外层本体
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), []byte("real-exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := entryRel(staging); got != exeName {
		t.Errorf("无内层时 entryRel 应回退外层: got %q", got)
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
	mkVersion("bcu_6.2.0", `{"installedAt":"2026-08-27 10:00:00","isImport":true,"source":"E:\\bcu"}`)
	mkVersion("bcu_6.1.0.1", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "bcu_6.0.0"), 0755)       // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	if list[0].Version != "6.2.0" { // Tree 扫描按版本号降序，最新在前
		t.Errorf("列表应最新在前: %+v", list)
	}
	byVer := map[string]BCUVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["6.2.0"]; !v.IsImport || v.InstalledAt != "2026-08-27 10:00:00" || v.Source != "E:\\bcu" {
		t.Errorf("6.2.0 元信息解析错误: %+v", v)
	}
	if v := byVer["6.1.0.1"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("6.1.0.1 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本
	if exe, err := m.ResolveExe("6.2.0"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(6.2.0): %v %v", exe, err)
	}
	if exe, err := m.ResolveExe("6.1.0.1"); err != nil || exe != filepath.Join(versionsDir, "bcu_6.1.0.1", exeName) {
		t.Errorf("无内层真身的旧布局应回退外层: %v %v", exe, err)
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove（Tree：rename 隔离后删除）
	if err := m.Remove("6.2.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

// TestResolveExePrefersInner 外层 BCUninstaller.exe 只是接力启动器（拉起真身后
// ~20ms 自退，不可当生命周期锚点——真机误判 external 事故回归，见 innerExeRel
// 注释与 TROUBLESHOOTING #24 接力家族）：启动路径与展示 ExePath 必须直指
// win-x64 真身；旧布局无内层回退外层；两者皆缺报错。
func TestResolveExePrefersInner(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	dir := filepath.Join(versionsDir, dirPrefix+"6.2.0")
	outer := filepath.Join(dir, exeName)
	inner := filepath.Join(dir, innerExeRel)

	// 仅外层（旧布局）：回退启动器本体
	os.MkdirAll(dir, 0755)
	os.WriteFile(outer, []byte("bootstrapper"), 0644)
	if got, err := m.ResolveExe("6.2.0"); err != nil || got != outer {
		t.Errorf("仅外层布局应回退外层: got=%q err=%v", got, err)
	}

	// 真身存在：启动与展示都指内层
	os.MkdirAll(filepath.Dir(inner), 0755)
	os.WriteFile(inner, []byte("real-app"), 0644)
	if got, err := m.ResolveExe("6.2.0"); err != nil || got != inner {
		t.Errorf("应优先内层真身: got=%q err=%v", got, err)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].ExePath != inner {
		t.Errorf("ListInstalled 展示 ExePath 应同为真身: %+v %v", list, err)
	}

	// 两者皆缺：报错（不可返回幽灵路径让 Start 才炸）
	os.Remove(inner)
	os.Remove(outer)
	if _, err := m.ResolveExe("6.2.0"); err == nil {
		t.Error("无任何可执行文件时必须报错")
	}
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, settingsName), []byte("fake-settings"), 0644)
	os.WriteFile(filepath.Join(src, "some.dll"), []byte("dll"), 0644)
	os.WriteFile(filepath.Join(src, "~lock.tmp"), []byte("lock"), 0644)
	os.WriteFile(filepath.Join(src, "desktop.ini"), []byte("ini"), 0644)
	os.MkdirAll(filepath.Join(src, "cache"), 0755)
	os.WriteFile(filepath.Join(src, "cache", "c1.dat"), []byte("cache"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底
	if !strings.HasPrefix(info.Version, "imported-") && !plainVersionRe.MatchString(info.Version) {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	for _, name := range []string{exeName, settingsName, "some.dll", "cache/c1.dat"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	for _, name := range []string{"~lock.tmp", "desktop.ini"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("垃圾文件 %s 不应被搬运", name)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "meta.json")); err != nil {
		t.Errorf("meta.json 未落盘: %v", err)
	}

	// 导入目录必须同时被版本树扫描半径收纳（列出、可解析、可卸载）
	list, lerr := m.ListInstalled()
	if lerr != nil || len(list) != 1 || list[0].Version != info.Version {
		t.Fatalf("导入版本应被 ListInstalled 收纳: %+v %v", list, lerr)
	}
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}
	if err := m.Remove(info.Version); err != nil {
		t.Errorf("兜底版本应可卸载: %v", err)
	}

	// 重复导入同一版本应被拒绝（重建目录后按真实版本走）
	src2 := t.TempDir()
	os.WriteFile(filepath.Join(src2, exeName), []byte("fake"), 0644)
	if _, err := m.ImportLocal(src2); err != nil {
		t.Fatalf("不同版本（不同兜底时间戳）导入应成功: %v", err)
	}

	// 源目录不含 exe → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Fatal("无 exe 的目录应报错")
	}
}
