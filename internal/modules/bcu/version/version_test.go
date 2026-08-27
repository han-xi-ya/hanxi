package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// 6.2.0 / 6.1.0.1 / 6.0.0 入列表（正确型态），tag 与资产版本不一致的 v9.9 丢弃，
// 缺 digest 的 v5.8 丢弃；v5.9 预发布保留。
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
	if v := byVer["6.1.0.1"]; v.Tag != "v6.1" {
		t.Errorf("6.1.0.1 解析错误: %+v", v)
	}
	if v := byVer["6.0.0"]; !strings.Contains(v.AssetName, "portable-x64") {
		t.Errorf("6.0.0 便携资产型态错误: %+v", v)
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
		if strings.Contains(r.AssetName, "setup") || strings.Contains(r.AssetName, "net8.0") {
			t.Errorf("混入非便携资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
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

func TestExtractAll(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeTestZip(t, map[string]string{
		exeName:       "fake-exe",
		"sub/dll.dll": "fake-dll",
	})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", exeName)); err != nil {
		t.Errorf("%s 未解压: %v", exeName, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "sub", "dll.dll")); err != nil {
		t.Errorf("子目录条目未解压: %v", err)
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

func TestExtractAllMissingExe(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"README.txt": "hello"})
	dst := filepath.Join(t.TempDir(), "dst")
	if err := extractAll(zipPath, dst); err == nil {
		t.Fatal("缺少 exe 的 zip 应被拒绝")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("失败后目标目录应被清理")
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
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove
	if err := m.Remove("6.2.0"); err != nil {
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

	// 兜底版本可解析/可卸载
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
