package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
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

// TestReleaseAssetsMatrix N13 形态矩阵：beta 槽位如实投影全部 3 资产（含变体），
// 平台整族 Windows（仅 Windows 产品不装全平台），zip 即官方页自述的便携形态，
// Setup.exe 安装器，托管所选 x64 zip 置 Managed 高亮位；版本号边界不串槽位。
func TestReleaseAssetsMatrix(t *testing.T) {
	list := parseReleases(fakeDownloadsPage)
	if len(list) != 2 {
		t.Fatalf("期望 2 槽位，实际 %d", len(list))
	}
	notes := list[1].Assets // 1.5.0.1422b：x64-Setup.exe + x64.zip（钮链/表行重复项须去重）+ ARM64.zip
	if len(notes) != 3 {
		t.Fatalf("期望 3 资产注记，实际 %d: %+v", len(notes), notes)
	}
	byName := map[string]bool{}
	managed := 0
	for _, n := range notes {
		if n.Platform != hostfeed.PlatformWindows {
			t.Errorf("资产 %s 平台应为 windows（Everything 仅 Windows 产品）: %+v", n.Label, n)
		}
		switch n.Label {
		case "Everything-1.5.0.1422b.x64.zip":
			if n.Form != hostfeed.FormPortable || !n.Managed {
				t.Errorf("托管便携 zip 注记错误: %+v", n)
			}
		case "Everything-1.5.0.1422b.x64-Setup.exe":
			if n.Form != hostfeed.FormInstaller || n.Managed {
				t.Errorf("Setup.exe 注记错误: %+v", n)
			}
		case "Everything-1.5.0.1422b.ARM64.zip":
			if n.Form != hostfeed.FormPortable || n.Managed {
				t.Errorf("ARM64 便携注记错误: %+v", n)
			}
		default:
			t.Errorf("意外资产（去重/边界失效？）: %s", n.Label)
		}
		if byName[n.Label] {
			t.Errorf("资产重复（去重失效）: %s", n.Label)
		}
		byName[n.Label] = true
		if n.Managed {
			managed++
		}
	}
	if managed != 1 {
		t.Errorf("Managed 高亮位应恰 1 条，实际 %d", managed)
	}
	// stable 槽位（.sha256 不在本夹具；x86/x64/Lite/en-US 共 4 条）不受 beta 行污染
	if len(list[0].Assets) != 4 {
		t.Errorf("stable 槽位注记数错误: %+v", list[0].Assets)
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

// TestPortableLayoutAnchor 大小写容错锚点自检（Commit 前模块策略）：
// 大写/小写 exe 均通过并回报实际文件名；空 exe 与缺 exe 拒绝。
func TestPortableLayoutAnchor(t *testing.T) {
	cases := []struct {
		name    string
		entries map[string]string
		wantErr bool
		wantExe string
	}{
		{"1.5 大写命名", map[string]string{"Everything.exe": "fake", "Everything.ini": "[Everything]"}, false, "Everything.exe"},
		{"1.4 小写命名", map[string]string{"everything.exe": "fake"}, false, "everything.exe"},
		{"缺 exe", map[string]string{"README.txt": "hello"}, true, ""},
		{"空 exe", map[string]string{"Everything.exe": ""}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range c.entries {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			exe, err := checkPortableLayout(dir)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if exe != c.wantExe {
				t.Errorf("回报 exe 名 = %q, want %q", exe, c.wantExe)
			}
		})
	}
}

func TestImportLocal(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

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

	// 重复导入同一源应被拒绝——确定性语义：假 PE 读不到 FileVersion，兜底
	// tag 为源指纹（绝对路径+尺寸+修改时刻哈希），两次调用 tag 必相同，
	// 第二次天然撞进"目标目录已存在 → 拒绝"查重分支。旧秒级时间戳形态靠
	// 预铺目录窗口模拟查重（跨秒即假红的 flake 源头），随产品语义修正一并
	// 作废，本断言不再赌时钟也不需预铺。
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("重复导入应报错")
	} else if !strings.Contains(err.Error(), src) && !strings.Contains(err.Error(), info.Dir) {
		// 兜底目录不进版本面板，报错必须自带目录路径给用户自助清理的抓手
		t.Errorf("兜底查重报错应指明残留目录: %v", err)
	}

	// 源内容真变化（exe 修改时刻漂移）→ 指纹更新 → 合法另立新目录
	if err := os.Chtimes(filepath.Join(src, "Everything.exe"), time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	info2, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("源变化后重复导入被误拦: %v", err)
	}
	if info2.Version == info.Version {
		t.Errorf("源内容变化后 tag 应更新，两次相同: %s", info2.Version)
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
	mkVersion("everything_v1.4.1.1032", "everything.exe", "")                             // 1.4 小写 exe + 无 meta
	os.MkdirAll(filepath.Join(versionsDir, "frp_v0.61.1"), 0755)                          // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "everything_v9.9.9"), 0755)                    // 缺 exe 的损坏安装必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "everything_vimported-20260101-000000"), 0755) // 导入兜底目录沿历史口径不列入
	os.MkdirAll(filepath.Join(versionsDir, "everything_2.0.0"), 0755)                     // 无 v 前缀外来目录不列入
	os.WriteFile(filepath.Join(versionsDir, "everything_2.0.0", "Everything.exe"), []byte("fake-exe"), 0644)

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
	if v := byVer["1.5.0.1422b"]; !v.IsImport || v.InstalledAt != "2026-08-26 10:00:00" || v.Source != "E:\\Everything" {
		t.Errorf("1.5 历史 map 账本解析错误: %+v", v)
	}
	if v := byVer["1.4.1.1032"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("1.4 默认元信息错误: %+v", v)
	}
	// 最新在前（剥 v 后按 versioncmp 数值分段排序）
	if list[0].Version != "1.5.0.1422b" {
		t.Errorf("列表应最新在前, got %s", list[0].Version)
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

	// Remove（Tree：rename 隔离后删除）
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
