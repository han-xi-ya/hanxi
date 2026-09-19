package version

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 x64/arm64 zip、setup.exe、apk/deb/dmg 跨平台资产、tag 不一致、缺 digest。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v0.8.96",
    "published_at": "2026-08-17T07:30:21Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "FlClash-0.8.96-windows-amd64.zip", "url": "https://api.github.com/x/1", "size": 58000000, "digest": "sha256:94a6683558e9b7ec02a3caface0b3f47a2a915d284987a1f6a77ed4681ff0b1b"},
      {"name": "FlClash-0.8.96-windows-amd64-setup.exe", "url": "https://api.github.com/x/2", "size": 36000000, "digest": "` + h('a') + `"},
      {"name": "FlClash-0.8.96-windows-arm64.zip", "url": "https://api.github.com/x/3", "size": 53000000, "digest": "` + h('b') + `"},
      {"name": "FlClash-0.8.96-android-arm64-v8a.apk", "url": "https://api.github.com/x/4", "size": 52000000, "digest": "` + h('c') + `"},
      {"name": "SHA256SUMS", "url": "https://api.github.com/x/5", "size": 1000, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v0.8.95",
    "published_at": "2026-08-01T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "FlClash-0.8.95-windows-amd64.zip", "url": "https://api.github.com/x/6", "size": 58000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v0.8.94",
    "published_at": "2026-07-01T00:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "FlClash-0.8.94-windows-amd64.zip", "url": "https://api.github.com/x/7", "size": 58000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v0.8.93",
    "published_at": "2026-06-01T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "FlClash-0.8.92-windows-amd64.zip", "url": "https://api.github.com/x/8", "size": 58000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v0.8.92",
    "published_at": "2026-05-01T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "FlClash-0.8.92-windows-amd64.zip", "url": "https://api.github.com/x/9", "size": 58000000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：
// 0.8.96/0.8.95 入列表（预发布 0.8.94 保留），tag 与资产版本不一致的 v0.8.93
// 丢弃，缺 digest 的 v0.8.92 丢弃；跨平台/arm64/setup 资产绝不混入。
// 缺 digest 即出列是"内核 artifact.Fetch 摘要必检恒可满足"的闸门（见 remote.go 实测注记）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]FlClashRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["0.8.96"]; v.Tag != "v0.8.96" ||
		v.SHA256 != "94a6683558e9b7ec02a3caface0b3f47a2a915d284987a1f6a77ed4681ff0b1b" ||
		v.Size != 58000000 || v.IsPre {
		t.Errorf("0.8.96 解析错误: %+v", v)
	}
	if v, ok := byVer["0.8.94"]; !ok || !v.IsPre {
		t.Errorf("预发布版本应保留并标记 IsPre: %+v", v)
	}
	for _, gone := range []string{"0.8.93", "0.8.92"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if !strings.Contains(r.AssetName, "windows-amd64.zip") {
			t.Errorf("混入非 x64 便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "flctest-*.zip")
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

// TestVersionFromToken 版本令牌形状：2~4 段纯数字与 imported-时间戳收纳（令牌
// 即展示版本，flclash 无 v 前缀），v 前缀/带字母尾段/中文目录名拒绝
// （与原 resolveVersionDir 白名单口径一致）。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"0.8.96", "0.8.96", true},
		{"0.8.96.1", "0.8.96.1", true},
		{"imported-20260826-150405", "imported-20260826-150405", true},
		{"v0.8.96", "", false}, // 带 v 前缀的目录名非本模块落位格式
		{"0.8.96b", "", false}, // 字母尾段非规整版本号
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

func TestUnpackWithFlClashLayout(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		exeName:               "fake-exe",
		"flutter_windows.dll": "fake-dll",
		"data/flutter_assets": "assets",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkLayout(staging); err != nil {
		t.Fatalf("checkLayout: %v", err)
	}
	for _, name := range []string{exeName, "flutter_windows.dll"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
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

func TestCheckLayoutMissingExe(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"flutter_windows.dll": "fake-dll"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	err := checkLayout(staging)
	if err == nil {
		t.Fatal("缺 FlClash.exe 应自检失败")
	}
	if !strings.Contains(err.Error(), "缺少可用的 FlClash.exe") {
		t.Errorf("错误信息应保持既有口径: %v", err)
	}
}

func TestCheckLayoutEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkLayout(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
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
	// 迁移前的历史账本（无 schema 的 map 形态）：导入账读 installedAt/isImport/source
	mkVersion("flclash_0.8.96", `{"installedAt":"2026-08-27 12:00:00","isImport":true,"source":"E:\\flclash"}`)
	mkVersion("flclash_0.8.95", "")
	os.MkdirAll(filepath.Join(versionsDir, "bcu_6.2.0"), 0755)      // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "flclash_0.8.94"), 0755) // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]FlClashVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["0.8.96"]; !v.IsImport || v.InstalledAt != "2026-08-27 12:00:00" || v.Source != "E:\\flclash" {
		t.Errorf("0.8.96 元信息解析错误: %+v", v)
	}
	if v := byVer["0.8.95"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("0.8.95 默认元信息错误: %+v", v)
	}

	if exe, err := m.ResolveExe("0.8.96"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(0.8.96): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("v0.8.96"); err == nil {
		t.Error("带 v 前缀非本模块版本形状，应报错")
	}
	if _, err := m.ResolveExe("0.8.93"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("0.8.96"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

// TestListInstalledReadsKernelLedger 新下载链的 artifact.Meta 账本（含 schema）：
// installedAt 由内核统一解析为展示串（Tree.Commit 必填；生产链不落零值账本，
// 夹具如实携带时刻），isImport/source 不携带。
func TestListInstalledReadsKernelLedger(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	installedAt := time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
	meta, err := json.Marshal(artifact.Meta{
		Schema:      artifact.DefaultSchema,
		Entry:       exeName,
		Version:     "0.8.97",
		ZipSHA256:   strings.Repeat("a", 64),
		AssetSHA256: strings.Repeat("b", 64),
		InstalledAt: installedAt,
		Source:      artifact.SourceRemote,
	})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(versionsDir, "flclash_0.8.97"), 0755)
	os.WriteFile(filepath.Join(versionsDir, "flclash_0.8.97", exeName), []byte("fake-exe"), 0644)
	os.WriteFile(filepath.Join(versionsDir, "flclash_0.8.97", "meta.json"), meta, 0644)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Version != "0.8.97" {
		t.Fatalf("内核账本目录应被列出: %+v", list)
	}
	if list[0].IsImport || list[0].Source != "" {
		t.Errorf("下载链不得携带导入语义: %+v", list[0])
	}
	if want := installedAt.Local().Format("2006-01-02 15:04:05"); list[0].InstalledAt != want {
		t.Errorf("installedAt 应经内核解析并保持展示口径: got %q want %q", list[0].InstalledAt, want)
	}
}

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, "flutter_windows.dll"), []byte("dll"), 0644)
	os.WriteFile(filepath.Join(src, "~lock.tmp"), []byte("lock"), 0644)
	os.WriteFile(filepath.Join(src, "desktop.ini"), []byte("ini"), 0644)
	os.MkdirAll(filepath.Join(src, "data"), 0755)
	os.WriteFile(filepath.Join(src, "data", "flutter_assets"), []byte("assets"), 0644)

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
	for _, name := range []string{exeName, "flutter_windows.dll", "data/flutter_assets"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	for _, name := range []string{"~lock.tmp", "desktop.ini"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("垃圾文件 %s 不应被搬运", name)
		}
	}

	// 兜底版本可解析/可卸载
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}
	if err := m.Remove(info.Version); err != nil {
		t.Errorf("兜底版本应可卸载: %v", err)
	}

	// 源目录不含 exe → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Fatal("无 exe 的目录应报错")
	}
}

// TestListingSkipsImportFallbackRecord ImportLocal 的兜底目录名也必须在
// ListInstalled 的扫描半径内（可被列出、可被卸载）。
func TestListingSkipsImportFallbackRecord(t *testing.T) {
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+"imported-20260826-150405")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, exeName), []byte("fake"), 0644)
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
