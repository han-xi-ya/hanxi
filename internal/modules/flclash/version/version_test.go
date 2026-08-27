package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestExtractAll(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, map[string]string{
		exeName:               "fake-exe",
		"flutter_windows.dll": "fake-dll",
		"data/flutter_assets": "assets",
	})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, name := range []string{exeName, "flutter_windows.dll"} {
		if _, err := os.Stat(filepath.Join(dir, "dst", name)); err != nil {
			t.Errorf("%s 未解压: %v", name, err)
		}
	}
}

func TestExtractAllZipSlip(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp("", "evil-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../evil.txt")
	w.Write([]byte("evil"))
	w2, _ := zw.Create(exeName)
	w2.Write([]byte("fake"))
	zw.Close()
	f.Close()

	dst := filepath.Join(dir, "dst")
	if err := extractAll(path, dst); err == nil {
		t.Fatal("ZipSlip 条目应被拒绝")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("失败后目标目录应被清理, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("恶意条目逃逸到了目标目录之外")
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
