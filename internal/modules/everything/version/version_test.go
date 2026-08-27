package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 与真实下载页同构的 HTML 片段：两个发布区块 + 各自资产变体 + 文件清单表
const fakeDownloadsPage = `<!DOCTYPE html><html><body>
<h2 id="dl" class="de">Download Everything 1.4.1.1032</h2>
<a style="width:256px;" class="button" href="/Everything-1.4.1.1032.x86.zip">Download Portable ZIP</a>
<a style="width:256px;" class="button" href="/Everything-1.4.1.1032.x64.zip">Download Portable ZIP 64-bit</a>
<a style="width:256px;" class="button" href="/Everything-1.4.1.1032.x64.Lite-Setup.exe">Download Lite Installer 64-bit</a>
<h2 id="dl15" class="de15">Download Everything 1.5.0.1422b Beta</h2>
<a id="dl15installer" style="width:256px;" class="button" href="/Everything-1.5.0.1422b.x64-Setup.exe">Download Installer 64-bit</a>
<a id="dl15portable" style="width:256px;" class="button" href="/Everything-1.5.0.1422b.x64.zip">Download Portable ZIP 64-bit</a>
<tr><td><span class="de"><a href="/Everything-1.4.1.1032.x64.zip">Everything-1.4.1.1032.x64.zip</a></span></td><td>Portable</td><td>x64</td></tr>
<tr><td><span class="de"><a href="/Everything-1.4.1.1032.x64.en-US.zip">Everything-1.4.1.1032.x64.en-US.zip</a></span></td><td>Portable</td><td>x64</td></tr>
<tr><td><span class="de15"><a href="/Everything-1.5.0.1422b.x64.zip">Everything-1.5.0.1422b.x64.zip</a></span></td><td>Portable</td><td>x64</td></tr>
<tr><td><span class="de15"><a href="/Everything-1.5.0.1422b.ARM64.zip">Everything-1.5.0.1422b.ARM64.zip</a></span></td><td>Portable</td><td>ARM64</td></tr>
</body></html>`

func TestParseReleases(t *testing.T) {
	list := parseReleases(fakeDownloadsPage)
	if len(list) != 2 {
		t.Fatalf("期望解析出 2 个槽位，实际 %d: %+v", len(list), list)
	}
	if list[0].Version != "1.4.1.1032" || list[0].Channel != "stable" {
		t.Errorf("槽位 0 错误: %+v", list[0])
	}
	if list[1].Version != "1.5.0.1422b" || list[1].Channel != "beta" {
		t.Errorf("槽位 1 错误: %+v", list[1])
	}
	for _, rel := range list {
		if !strings.Contains(rel.AssetURL, "Everything-"+rel.Version+".x64.zip") {
			t.Errorf("AssetURL 构造错误: %s", rel.AssetURL)
		}
		// 变体（en-US/ARM/Lite/x86）绝不能混入
		if strings.Contains(rel.AssetURL, "en-US") || strings.Contains(rel.AssetURL, "ARM") || strings.Contains(rel.AssetURL, "Lite") || strings.Contains(rel.AssetURL, "x86") {
			t.Errorf("混入变体资产: %s", rel.AssetURL)
		}
	}
}

func TestParseReleasesEmpty(t *testing.T) {
	if list := parseReleases("<html>无版本区块的页面</html>"); len(list) != 0 {
		t.Fatalf("期望空列表，实际 %+v", list)
	}
}

func TestFindSHAInManifest(t *testing.T) {
	manifest := "c42efad041d4c0bb4d4ac97ae7cbe89f153ec1fe078772392e749c7f5d5282d3 *Everything-1.4.1.1032.x64-Setup.exe\n" +
		"698df475ec44e638f66f1b6a32d28fea613cec78d3b6310e6abe53431eeb940c *Everything-1.4.1.1032.x64.zip\n" +
		"97b057cb3211192f0c821e0a7bf602c6b6a4173f7595a22d6174c03d2b4d301f  Everything-1.4.1.1032.x64.en-US.zip\n"
	if got := findSHAInManifest(manifest, "Everything-1.4.1.1032.x64.zip"); got != "698df475ec44e638f66f1b6a32d28fea613cec78d3b6310e6abe53431eeb940c" {
		t.Errorf("哈希不匹配, 实际 %q", got)
	}
	if got := findSHAInManifest(manifest, "Everything-9.9.9.x64.zip"); got != "" {
		t.Errorf("不存在条目应返回空, 实际 %q", got)
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "evtest-*.zip")
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
		"Everything.exe": "fake-exe",
		"Everything.lng": "fake-lng",
	})
	if err := extractAll(zipPath, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "Everything.exe")); err != nil {
		t.Errorf("Everything.exe 未解压: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "Everything.lng")); err != nil {
		t.Errorf("Everything.lng 未解压: %v", err)
	}

	// 小写 exe 命名（1.4 通道）同样通过自检
	dir2 := t.TempDir()
	zip2 := makeTestZip(t, map[string]string{"everything.exe": "fake"})
	if err := extractAll(zip2, filepath.Join(dir2, "dst")); err != nil {
		t.Fatalf("小写 exe 自检失败: %v", err)
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
	w2, _ := zw.Create("Everything.exe")
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

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	// 构造本地便携安装目录：exe + 全套数据 + 需跳过的临时文件
	src := t.TempDir()
	writeFile := func(name, content string) { os.WriteFile(filepath.Join(src, name), []byte(content), 0644) }
	writeFile("Everything.exe", "fake-exe-bytes")
	writeFile("Everything.ini", "[Everything]\n")
	writeFile("Everything.lng", "lang")
	writeFile("Everything.db", "db")
	writeFile("Session.json", "{}")
	writeFile("Session.json.tmp", "temp")
	writeFile("~lock.tmp", "lock")
	os.MkdirAll(filepath.Join(src, "Plugins"), 0755)
	writeFile("Plugins/plugin.txt", "p")

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion）
	if !strings.HasPrefix(info.Version, "imported-") && !plainVersionRe.MatchString(info.Version) {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	for _, name := range []string{"Everything.exe", "Everything.ini", "Everything.lng", "Everything.db", "Session.json", "Plugins/plugin.txt"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	for _, name := range []string{"Session.json.tmp", "~lock.tmp"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("临时文件 %s 不应被搬运", name)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "meta.json")); err != nil {
		t.Errorf("meta.json 未落盘: %v", err)
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

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir string, exe string, meta string) {
		os.MkdirAll(filepath.Join(versionsDir, dir), 0755)
		os.WriteFile(filepath.Join(versionsDir, dir, exe), []byte("fake-exe"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("everything_v1.5.0.1422b", "Everything.exe",
		`{"installedAt":"2026-08-26 10:00:00","isImport":true,"source":"E:\\Everything"}`)
	mkVersion("everything_v1.4.1.1032", "everything.exe", "")          // 1.4 小写 exe + 无 meta
	os.MkdirAll(filepath.Join(versionsDir, "frp_v0.61.1"), 0755)       // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "everything_v9.9.9"), 0755) // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]EverythingVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["1.5.0.1422b"]; !v.IsImport || v.InstalledAt != "2026-08-26 10:00:00" {
		t.Errorf("1.5 元信息解析错误: %+v", v)
	}
	if v := byVer["1.4.1.1032"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("1.4 默认元信息错误: %+v", v)
	}

	// ResolveExe 大小写不敏感（Windows 上候选名以 Everything.exe 命中小写文件亦合法）
	if exe, err := m.ResolveExe("1.4.1.1032"); err != nil || !strings.EqualFold(filepath.Base(exe), "everything.exe") {
		t.Errorf("ResolveExe(1.4): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("非法版本"); err == nil {
		t.Error("非法版本号应报错")
	}

	// Remove
	if err := m.Remove("1.4.1.1032"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
}

func TestSnapshotReleasesSanity(t *testing.T) {
	if len(snapshotReleases) == 0 {
		t.Fatal("内置快照不能为空（离线最后防线）")
	}
	for _, r := range snapshotReleases {
		if !plainVersionRe.MatchString(r.Version) {
			t.Errorf("快照版本号格式异常: %q", r.Version)
		}
		if r.SHA256 == "" || len(r.SHA256) != 64 {
			t.Errorf("快照 %s 缺 sha256（官方哈希是下载校验主依据）", r.Version)
		}
		if !r.Stale {
			t.Errorf("快照必须标记 stale: %s", r.Version)
		}
	}
	// 快照间版本唯一
	seen := map[string]bool{}
	for _, r := range snapshotReleases {
		if seen[r.Version] {
			t.Errorf("快照版本重复: %s", r.Version)
		}
		seen[r.Version] = true
	}
}
