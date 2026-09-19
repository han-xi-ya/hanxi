package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应，形态取自实测上游
// （v0.8.0 资产清单）：stable、beta（tag 带 -beta.N 且 prerelease=true）、
// mac zip 炸弹（Paseo-<ver>-<arch>.zip 缺 Setup 前缀，绝不能收）、
// NSIS exe / blockmap / latest.yml 干扰项、缺 digest 发布、缺 win zip 发布、
// 非语义 tag。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	arch := localWinArch()
	if arch == "" {
		t.Skip("当前平台 GOARCH 无 Windows 产物映射，资产选择逻辑不适用")
	}
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v0.8.0",
    "published_at": "2026-09-10T10:39:19Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Paseo-0.8.0-` + arch + `.zip", "url": "https://api.github.com/x/1", "size": 181396869, "digest": "` + h('a') + `"},
      {"name": "Paseo-Setup-0.8.0.exe", "url": "https://api.github.com/x/2", "size": 276446178, "digest": "` + h('b') + `"},
      {"name": "Paseo-Setup-0.8.0-` + arch + `.exe", "url": "https://api.github.com/x/3", "size": 138271232, "digest": "` + h('c') + `"},
      {"name": "Paseo-Setup-0.8.0-` + arch + `.exe.blockmap", "url": "https://api.github.com/x/4", "size": 145279, "digest": "` + h('d') + `"},
      {"name": "Paseo-Setup-0.8.0-` + arch + `.zip", "url": "https://api.github.com/x/5", "size": 186136244, "digest": "` + h('e') + `"},
      {"name": "latest.yml", "url": "https://api.github.com/x/6", "size": 674, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v0.8.0-beta.1",
    "published_at": "2026-09-08T09:21:56Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "Paseo-Setup-0.8.0-beta.1-` + arch + `.zip", "url": "https://api.github.com/x/7", "size": 177500000, "digest": "` + h('g') + `"}
    ]
  },
  {
    "tag_name": "v0.7.2",
    "published_at": "2026-09-02T00:11:20Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Paseo-Setup-0.7.2-` + arch + `.zip", "url": "https://api.github.com/x/8", "size": 159100000, "digest": "sha256:not-a-real-digest"}
    ]
  },
  {
    "tag_name": "v0.7.0",
    "published_at": "2026-08-31T17:29:06Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Paseo-Setup-0.7.0-` + arch + `.exe", "url": "https://api.github.com/x/9", "size": 115100000, "digest": "` + h('i') + `"},
      {"name": "Paseo-x86_64.AppImage", "url": "https://api.github.com/x/10", "size": 142800000, "digest": "` + h('j') + `"}
    ]
  },
  {
    "tag_name": "nightly-20260901",
    "published_at": "2026-09-01T00:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "Paseo-Setup-nightly-20260901-` + arch + `.zip", "url": "https://api.github.com/x/11", "size": 180000000, "digest": "` + h('k') + `"}
    ]
  }
]`
	return []byte(body)
}

func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// v0.8.0（zip+digest）、v0.8.0-beta.1 入选；v0.7.2 缺合法 digest、
	// v0.7.0 无 win zip、nightly tag 非语义 → 全部拒收
	if len(list) != 2 {
		t.Fatalf("期望 2 条，实际 %d: %+v", len(list), list)
	}
	if list[0].Version != "0.8.0" || list[0].Tag != "v0.8.0" || list[0].IsPre {
		t.Errorf("stable 条目解析异常: %+v", list[0])
	}
	arch := localWinArch()
	if list[0].AssetName != "Paseo-Setup-0.8.0-"+arch+".zip" {
		t.Errorf("应选 win zip（带 Setup 前缀），实际 %s——mac zip 炸弹被误收", list[0].AssetName)
	}
	if !strings.HasPrefix(list[0].SHA256, "e") || len(list[0].SHA256) != 64 {
		t.Errorf("SHA256 剥离 digest 前缀异常: %q", list[0].SHA256)
	}
	if !list[1].IsPre || list[1].Version != "0.8.0-beta.1" {
		t.Errorf("beta 条目解析异常: %+v", list[1])
	}
}

func TestListRemoteChannelFilter(t *testing.T) {
	parseAndSeed(t)
	all, err := remoteCache.get(true)
	if err != nil || len(all) != 2 {
		t.Fatalf("全量通道异常: %d %v", len(all), err)
	}
	stable, err := remoteCache.get(false)
	if err != nil || len(stable) != 1 || stable[0].Version != "0.8.0" {
		t.Fatalf("stable 通道裁剪异常: %+v %v", stable, err)
	}
	if _, ok := remoteCache.findRelease("0.8.0-beta.1"); !ok {
		t.Errorf("findRelease 应按裸版本号命中缓存条目")
	}
}

// parseAndSeed 以样例响应回填进程级缓存（后续测试直接命中，不走网络）。
func parseAndSeed(t *testing.T) {
	t.Helper()
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	remoteCache.mu.Lock()
	remoteCache.data = list
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
}

func TestCompareSemver(t *testing.T) {
	seq := []string{"0.7.2-beta.1", "0.7.2", "0.8.0-beta.1", "0.8.0", "0.8.1", "imported-x"}
	for i := 0; i+1 < len(seq)-1; i++ {
		if Compare(seq[i], seq[i+1]) >= 0 {
			t.Errorf("期望 %s < %s", seq[i], seq[i+1])
		}
	}
	if Compare("0.8.0-beta.2", "0.8.0-beta.1") != 1 {
		t.Error("beta 序号比较异常")
	}
	if Compare("v0.8.0", "0.8.0") != 0 {
		t.Error("v 前缀应被容忍")
	}
	if Compare("imported-a", "imported-b") != -1 {
		t.Error("非规范版本应退化字典序")
	}
}

func TestTokenVersionRe(t *testing.T) {
	ok := []string{"0.8.0", "0.8.0-beta.1", "imported-20260915-010203"}
	bad := []string{"", "x", "0.8", "0.8.0.zip", "-1", "v0.8.0", "imported-2026", "../../../windows"}
	for _, s := range ok {
		if !tokenVersionRe.MatchString(s) {
			t.Errorf("应接受 %s", s)
		}
	}
	for _, s := range bad {
		if tokenVersionRe.MatchString(s) {
			t.Errorf("应拒绝 %s", s)
		}
	}
}

// ---------- 内核解包 + 模块双锚点自检（替代原 extractAll 时代的用例） ----------

// makeTestZip 构造测试用 zip（win-unpacked 形态中央目录无根包裹：
// Paseo.exe 落在包根，勿按"包内单根目录"直觉改造）。
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "pasctest-*.zip")
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

func TestUnpackWithElectronAnchors(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		"Paseo.exe":                     "MZ fake exe",
		"resources/app.asar":            "asar",
		"resources/app-dist/index.html": "<html>",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := layoutCheck(staging); err != nil {
		t.Fatalf("layoutCheck: %v", err)
	}
	for _, name := range []string{"Paseo.exe", "resources/app.asar", "resources/app-dist/index.html"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"../escaped.txt": "x"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

func TestLayoutCheckMissingAsar(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"Paseo.exe": "fake-exe"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	err := layoutCheck(staging)
	if err == nil {
		t.Fatal("缺 resources/app.asar 应自检失败")
	}
	if !strings.Contains(err.Error(), "app.asar") {
		t.Errorf("错误信息应指出缺失主包: %v", err)
	}
}

func TestLayoutCheckEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, asarRelPath), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := layoutCheck(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
	}
}

// ---------- 版本树扫描 / 解析 / 卸载（委托 Tree） ----------

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, asar string, exeContent string, meta string) {
		full := filepath.Join(versionsDir, dir)
		if err := os.MkdirAll(filepath.Join(full, "resources"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(full, exeName), []byte(exeContent), 0644); err != nil {
			t.Fatal(err)
		}
		if asar != "" {
			if err := os.WriteFile(filepath.Join(full, asarRelPath), []byte(asar), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if meta != "" {
			if err := os.WriteFile(filepath.Join(full, "meta.json"), []byte(meta), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkVersion("paseo_0.8.0", "asar", "exe080",
		`{"installedAt":"2026-09-15 10:00:00","isImport":true,"source":"E:\\Paseo","verifiedHash":false}`)
	mkVersion("paseo_0.7.2", "asar", "exe072", "")
	mkVersion("paseo_0.8.0-beta.1", "asar", "exeBeta", "")
	mkVersion("paseo_broken-noasar", "", "exe", "") // 缺 app.asar 的损坏安装必须跳过
	mkVersion("paseo_0.9.0", "asar", "", "")        // 空 exe 的损坏安装必须跳过
	if err := os.MkdirAll(filepath.Join(versionsDir, "vscode_1.0"), 0755); err != nil {
		t.Fatal(err) // 异模块目录必须跳过
	}
	if err := os.MkdirAll(filepath.Join(versionsDir, "paseo_x"), 0755); err != nil {
		t.Fatal(err) // 形状外令牌必须跳过
	}

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	byVer := map[string]PaseoVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	// 历史导入账本（无 schema map）：installedAt 原样、isImport/source/verifiedHash 收纳
	if v := byVer["0.8.0"]; !v.IsImport || v.InstalledAt != "2026-09-15 10:00:00" ||
		v.Source != `E:\Paseo` || v.VerifiedHash {
		t.Errorf("0.8.0 导入账本解析错误: %+v", v)
	}
	if v := byVer["0.7.2"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("0.7.2 默认元信息错误: %+v", v)
	}

	// ResolveExe / 非法版本 / 路径穿越
	if exe, err := m.ResolveExe("v0.7.2"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(带 v 前缀): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("0.7.2"); err != nil {
		t.Errorf("裸版本应可解析: %v", err)
	}
	if _, err := m.ResolveExe("0.8.0-beta.1"); err != nil {
		t.Errorf("预发布版本应可解析: %v", err)
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("0.7"); err == nil {
		t.Error("形状外版本号应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	// Remove（委托 Tree：隔离后删除）
	if err := m.Remove("0.7.2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 2 {
		t.Errorf("卸载后应剩 2 个版本，实际 %d", len(list))
	}
}

// ---------- 本地导入（整套 Electron 目录迁移） ----------

func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, asarRelPath), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "resources", "default_app.asar"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("noise"), 0644); err != nil {
		t.Fatal(err)
	}
	// 顶层 meta 噪声文件不搬运（skip 名单）
	if err := os.WriteFile(filepath.Join(src, "meta.json"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion）
	if !strings.HasPrefix(info.Version, "imported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src || info.VerifiedHash {
		t.Errorf("导入标记错误: %+v", info)
	}
	// 程序目录整套迁移：exe + resources/* 齐备
	for _, rel := range []string{exeName, asarRelPath, filepath.Join("resources", "default_app.asar"), "readme.txt"} {
		if _, err := os.Stat(filepath.Join(info.Dir, rel)); err != nil {
			t.Errorf("导入后缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "meta.json")); err != nil {
		t.Errorf("导入账本应重写落位: %v", err)
	}

	// 兜底版本必须可以从目录名解析并支持卸载
	dir := filepath.Join(m.versionsDir, dirPrefix+info.Version)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("兜底版本目录不存在: %v", err)
	}
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}

	// 重复导入同版本应被拒绝；若二次导入跨入新时间戳秒（目录名不同）则属
	// 合法新收纳，断言其目录与首次不同即可。
	if info2, err := m.ImportLocal(src); err == nil && info2.Dir == info.Dir {
		t.Error("命中同名目标目录时重复导入应报错")
	} else if err != nil && !strings.Contains(err.Error(), "已存在") {
		t.Errorf("重复导入错误口径异常: %v", err)
	}

	// 缺 exe / 缺 app.asar 的源目录应报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 exe 的目录应报错")
	}
	noAsar := t.TempDir()
	if err := os.WriteFile(filepath.Join(noAsar, exeName), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportLocal(noAsar); err == nil {
		t.Error("缺 app.asar 的目录应报错")
	}
}

func TestImportRefusesHostedTreeSelf(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	src := filepath.Join(versionsDir, "paseo_0.8.0")
	if err := os.MkdirAll(filepath.Join(src, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, exeName), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, asarRelPath), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "托管目录内") {
		t.Fatalf("托管目录内的源应拒绝导入, got %v", err)
	}
}

func TestIsUnderDir(t *testing.T) {
	base := filepath.Join("D:", "hanxi")
	if !isUnderDir(filepath.Join(base, "versions", "paseo_0.8.0"), base) {
		t.Error("子路径应判定为目录内")
	}
	if isUnderDir(filepath.Join("D:", "other"), base) {
		t.Error("旁支路径不应命中")
	}
	if isUnderDir(base, base) {
		t.Error("自身不算 under")
	}
}
