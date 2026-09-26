package version

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

func TestParseDownloadPage(t *testing.T) {
	html := `
	<tr><td>2026-08-01</td><td><a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x64.zip">64-bit</a></td></tr>
	<a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x86.zip">32-bit</a>
	<a href="https://download.snipaste.com/archives/Snipaste-2.9.2-Beta-x64.zip">64-bit</a>
	<a href="https://evil.example/archives/Snipaste-9.9.9-x64.zip">evil</a>`
	list := parseDownloadPage(html, downloadsPageURL)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(list), list)
	}
	if list[0].Version != "2.11.3" || list[0].IsPre {
		t.Fatalf("stable first = %#v", list[0])
	}
	if list[1].Version != "2.9.2-Beta" || !list[1].IsPre {
		t.Fatalf("beta = %#v", list[1])
	}
}

// 与真实下载页同构：当前版钮链+历史表重复行+跨平台变体+Beta 双线+截断版号
// 陷阱+非官方域+校验附属件——资产命名全部取自 2026-09-26 官网实测 href。
const fakeMatrixPage = `
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x64.zip" title="Snipaste-2.11.3-x64.zip">64-bit</a>
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3.dmg" title="Snipaste-2.11.3.dmg">Universal</a>
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x86_64.AppImage" title="Snipaste-2.11.3-x86_64.AppImage">AppImage</a>
<tr><td><a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x64.zip">x64</a></td>
<td><a href="https://download.snipaste.com/archives/Snipaste-2.11.3-x86.zip">x86</a></td>
<td><a href="https://download.snipaste.com/archives/Snipaste-2.11.3-arm64.zip">ARM64</a></td></tr>
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3.sha1">sha-1 附属件</a>
<a href="https://evil.example/archives/Snipaste-2.11.3-mac.zip">非官方域同名陷阱</a>
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3-Beta-x64.zip">beta64</a>
<a href="https://download.snipaste.com/archives/Snipaste-2.11.3-Beta.dmg">betamac</a>
<a href="https://download.snipaste.com/archives/Snipaste-2.11-x64.zip">截断版号</a>
<a href="https://download.snipaste.com/archives/Snipaste-1.16.2-x64.zip">v1 x64</a>
<a href="https://download.snipaste.com/archives/Snipaste-1.16.2-x86.zip">v1 x86</a>
<a href="https://download.snipaste.com/archives/Snipaste-1.16.2-XP.zip">v1 xp</a>`

// assertMatrix 校验一条槽位注记：Label→平台/形态期望表逐条命中、不多不少、
// 无重复，Managed 高亮恰一（等于本槽 chosen）。
func assertMatrix(t *testing.T, rel SnipasteRelease, want map[string][2]string) {
	t.Helper()
	if len(rel.Assets) != len(want) {
		t.Fatalf("槽位 %s 注记数 %d，期望 %d: %+v", rel.Version, len(rel.Assets), len(want), rel.Assets)
	}
	seen := map[string]bool{}
	managed := 0
	for _, n := range rel.Assets {
		exp, ok := want[n.Label]
		if !ok {
			t.Errorf("槽位 %s 意外资产（去重/边界/域名/附属件闸门失效？）: %s", rel.Version, n.Label)
			continue
		}
		if seen[n.Label] {
			t.Errorf("资产重复（去重失效）: %s", n.Label)
		}
		seen[n.Label] = true
		if string(n.Platform) != exp[0] {
			t.Errorf("资产 %s 平台 %s，期望 %s", n.Label, n.Platform, exp[0])
		}
		if string(n.Form) != exp[1] {
			t.Errorf("资产 %s 形态 %s，期望 %s", n.Label, n.Form, exp[1])
		}
		if expManaged := n.Label == rel.AssetName; n.Managed != expManaged {
			t.Errorf("资产 %s Managed=%v，期望 %v", n.Label, n.Managed, expManaged)
		}
		if n.Managed {
			managed++
		}
	}
	if managed != 1 {
		t.Errorf("槽位 %s Managed 高亮位应恰 1 条，实际 %d", rel.Version, managed)
	}
}

// TestReleaseAssetsMatrix N13 形态矩阵：官网 archives 页每版实测最多 5 资产
// （x86/x64/arm64 zip + dmg + AppImage），zip 整族直判 Windows 便携（win-arm64
// 频道 302 落 -arm64.zip 实证），dmg/AppImage 交 Classify 判 mac 安装器/linux
// 便携；.sha1 附属件、非官方域同名、Beta/stable 同前缀互斥、截断版号（"2.11"
// 不得认领 "Snipaste-2.11.3-x64.zip"）四类噪声全部拒入。
func TestReleaseAssetsMatrix(t *testing.T) {
	list := parseDownloadPage(fakeMatrixPage, downloadsPageURL)
	byVersion := map[string]SnipasteRelease{}
	for _, rel := range list {
		byVersion[rel.Version] = rel
	}
	if len(list) != 4 {
		t.Fatalf("期望 4 槽位（2.11.3 / 2.11.3-Beta / 2.11 / 1.16.2），实际 %d: %+v", len(list), list)
	}

	assertMatrix(t, byVersion["2.11.3"], map[string][2]string{
		"Snipaste-2.11.3-x64.zip":         {"windows", "portable"}, // 托管所选，高亮恰一
		"Snipaste-2.11.3-x86.zip":         {"windows", "portable"},
		"Snipaste-2.11.3-arm64.zip":       {"windows", "portable"},
		"Snipaste-2.11.3.dmg":             {"macos", "installer"},
		"Snipaste-2.11.3-x86_64.AppImage": {"linux", "portable"},
	})
	assertMatrix(t, byVersion["2.11.3-Beta"], map[string][2]string{
		"Snipaste-2.11.3-Beta-x64.zip": {"windows", "portable"}, // Beta 槽 own chosen
		"Snipaste-2.11.3-Beta.dmg":     {"macos", "installer"},
	})
	assertMatrix(t, byVersion["2.11"], map[string][2]string{
		"Snipaste-2.11-x64.zip": {"windows", "portable"}, // 仅本名，".3-x64.zip" 被截断串槽闸拒收
	})
	assertMatrix(t, byVersion["1.16.2"], map[string][2]string{
		"Snipaste-1.16.2-x64.zip": {"windows", "portable"},
		"Snipaste-1.16.2-x86.zip": {"windows", "portable"},
		"Snipaste-1.16.2-XP.zip":  {"windows", "portable"}, // XP 变体同走 arch 后缀约定
	})

	// stable 槽不认领 Beta 命名变体；.sha1 附属件与非官方域同名均被闸掉
	for _, n := range byVersion["2.11.3"].Assets {
		lower := strings.ToLower(n.Label)
		if strings.Contains(lower, "beta") {
			t.Errorf("stable 槽混入 Beta 变体: %s", n.Label)
		}
		if strings.HasSuffix(lower, ".sha1") || strings.Contains(lower, "mac.zip") {
			t.Errorf("噪声条目漏网（附属件/域名闸失效）: %s", n.Label)
		}
	}
}

func TestFindHashInManifest(t *testing.T) {
	manifest := "850bd133114a6b24156d19e41a06f057555b21b5 *Snipaste-2.11.3-x64.zip\n" +
		"bf69d62c6198296153766d16ea83c94b2443dd1f *Snipaste-2.11.3-x86.zip\n"
	got := findHashInManifest(manifest, "Snipaste-2.11.3-x64.zip")
	if got != "850bd133114a6b24156d19e41a06f057555b21b5" {
		t.Fatalf("hash = %q", got)
	}
}

func TestReleaseCacheReturnsStaleData(t *testing.T) {
	cache := newReleaseCache(remoteSource{
		client: &http.Client{Timeout: time.Second}, downloadPage: "http://127.0.0.1:1", manifestURL: "http://127.0.0.1:1",
	})
	cache.data = []SnipasteRelease{{Version: "2.11.3"}}
	cache.fetchedAt = time.Now().Add(-2 * cacheTTL)
	list, err := cache.get()
	if err != nil || len(list) != 1 || !list[0].Stale {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}

func TestRemoteSourceFetchRemote(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download.html":
			_, _ = w.Write([]byte(`<a href="` + server.URL + `/archives/Snipaste-2.11.3-x64.zip">64-bit</a>`))
		case "/sha-1.txt":
			_, _ = w.Write([]byte("850bd133114a6b24156d19e41a06f057555b21b5 *Snipaste-2.11.3-x64.zip\n"))
		case "/archives/Snipaste-2.11.3-x64.zip":
			w.Header().Set("Content-Length", "123")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// 本测试只验证官网解析链；测试服务器不是官方域名，因此直接验证解析/清单函数。
	body, _ := fetchPage(server.Client(), server.URL+"/download.html", maxPageBody)
	body = bytes.ReplaceAll(body, []byte(server.URL), []byte("https://download.snipaste.com"))
	list := parseDownloadPage(string(body), downloadsPageURL)
	if len(list) != 1 {
		t.Fatalf("list=%#v", list)
	}
}

// TestUnpackAndLocateLayouts 解包已委托内核 artifact.UnpackZip（恶意 zip 闸门
// 由 packages/go/artifact 自测覆盖）；本测试锁定"解包 + Snipaste 布局自检"的
// 组合行为：官方双层布局可用、ZipSlip 被拒。
func TestUnpackAndLocateLayouts(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "ok.zip")
	writeZip(t, zipPath, map[string][]byte{
		"Snipaste-2.11.3/Snipaste.exe": []byte("exe"),
		"Snipaste-2.11.3/config.ini":   []byte("cfg"),
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	root, err := locateInstallRoot(staging)
	if err != nil || filepath.Base(root) != "Snipaste-2.11.3" {
		t.Fatalf("root=%q err=%v", root, err)
	}

	badZip := filepath.Join(t.TempDir(), "bad.zip")
	writeZip(t, badZip, map[string][]byte{"../evil.txt": []byte("x"), "Snipaste.exe": []byte("exe")})
	if err := artifact.UnpackZip(badZip, filepath.Join(t.TempDir(), "bad"), artifact.DefaultLimits, nil); err == nil {
		t.Fatal("ZipSlip should fail")
	}
}

func TestLocateInstallRootRejectsMissingMultipleOrDeep(t *testing.T) {
	cases := []struct {
		name  string
		files map[string][]byte
	}{
		{"missing exe", map[string][]byte{"readme.txt": []byte("x")}},
		{"multiple exe", map[string][]byte{"Snipaste.exe": []byte("x"), "nested/Snipaste.exe": []byte("y")}},
		{"too deep", map[string][]byte{"a/b/Snipaste.exe": []byte("y")}},
		{"empty exe", map[string][]byte{"Snipaste.exe": {}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			zipPath := filepath.Join(t.TempDir(), "case.zip")
			writeZip(t, zipPath, c.files)
			staging := filepath.Join(t.TempDir(), "staging")
			if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
				t.Fatalf("UnpackZip: %v", err)
			}
			if _, err := locateInstallRoot(staging); err == nil {
				t.Fatalf("%s should fail", c.name)
			}
		})
	}
}

func TestDownloadFailures(t *testing.T) {
	// 失败注入（真 tmp 目录）：非法版本、远程解析失败、目标已安装三类前置闸。
	t.Run("illegal version rejected before IO", func(t *testing.T) {
		versionsDir := t.TempDir()
		m := NewManager(versionsDir)
		if err := m.Download("../evil", nil); err == nil {
			t.Fatal("path traversal version should fail")
		}
		ents, _ := os.ReadDir(versionsDir)
		if len(ents) != 0 {
			t.Fatalf("非法版本不应触盘: %v", ents)
		}
	})
	t.Run("remote resolve failure propagates", func(t *testing.T) {
		m := NewManager(t.TempDir())
		m.cache = newReleaseCache(remoteSource{
			client: &http.Client{Timeout: time.Second}, downloadPage: "http://127.0.0.1:1", manifestURL: "http://127.0.0.1:1",
		})
		if err := m.Download("2.11.3", nil); err == nil {
			t.Fatal("unreachable official site should fail")
		}
	})
	t.Run("already installed rejected", func(t *testing.T) {
		versionsDir := t.TempDir()
		dir := filepath.Join(versionsDir, dirPrefix+"2.11.3")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, exeName), []byte("exe"), 0644); err != nil {
			t.Fatal(err)
		}
		m := NewManager(versionsDir)
		if err := m.Download("2.11.3", nil); err == nil || !strings.Contains(err.Error(), "已安装") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestManagerImportListResolveRemove(t *testing.T) {
	versionsDir := t.TempDir()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, exeName), []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config.ini"), []byte("cfg"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(versionsDir)
	m.fileVersion = func(string) (string, error) { return "2.11.3", nil }
	info, err := m.ImportLocal(source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "2.11.3" {
		t.Fatalf("version=%q", info.Version)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "config.ini")); err != nil {
		t.Fatalf("config not copied: %v", err)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	if _, err := m.ResolveExe("../2.11.3"); err == nil {
		t.Fatal("path traversal version should fail")
	}
	if err := m.Remove("2.11.3"); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
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
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
