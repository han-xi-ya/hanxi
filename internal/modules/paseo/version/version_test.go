package version

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestVersionDirRe(t *testing.T) {
	ok := []string{"paseo_0.8.0", "paseo_0.8.0-beta.1", "paseo_imported-20260915-010203"}
	bad := []string{"paseo_", "paseo_x", "paseo_0.8", "paseo_0.8.0.zip", "vscode_1.0", "paseo_-1"}
	for _, s := range ok {
		if !versionDirRe.MatchString(s) {
			t.Errorf("应接受 %s", s)
		}
	}
	for _, s := range bad {
		if versionDirRe.MatchString(s) {
			t.Errorf("应拒绝 %s", s)
		}
	}
}

func TestExtractAllLayoutAndSlip(t *testing.T) {
	dir := t.TempDir()

	// 合法 win-unpacked 布局：根目录直落 Paseo.exe + resources/app.asar（无根包裹）
	good := filepath.Join(dir, "good.zip")
	writeTestZip(t, good, map[string][]byte{
		"Paseo.exe":                     []byte("MZ fake exe"),
		"resources/app.asar":            []byte("asar"),
		"resources/app-dist/index.html": []byte("<html>"),
	})
	target := filepath.Join(dir, "out")
	if err := extractAll(good, target); err != nil {
		t.Fatalf("合法 zip 解压失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, exeName)); err != nil {
		t.Fatalf("Paseo.exe 应落在目标根目录: %v", err)
	}

	// ZipSlip：逃逸条目必须拒绝并清理目标目录
	evil := filepath.Join(dir, "evil.zip")
	writeTestZip(t, evil, map[string][]byte{
		"../escaped.txt": []byte("x"),
		"Paseo.exe":      []byte("MZ"),
	})
	badTarget := filepath.Join(dir, "bad")
	if err := extractAll(evil, badTarget); err == nil {
		t.Fatal("ZipSlip 条目应被拒绝")
	}
	if _, err := os.Stat(badTarget); !os.IsNotExist(err) {
		t.Error("解压失败后应清理目标目录")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Error("逃逸文件不得落盘")
	}

	// 布局自检：缺 app.asar 拒绝
	nolayout := filepath.Join(dir, "nolayout.zip")
	writeTestZip(t, nolayout, map[string][]byte{"Paseo.exe": []byte("MZ")})
	if err := extractAll(nolayout, filepath.Join(dir, "out2")); err == nil {
		t.Error("缺 resources/app.asar 的布局应自检失败")
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

func writeTestZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
