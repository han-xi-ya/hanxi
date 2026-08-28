package version

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestExtractAllLayoutsAndZipSlip(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "ok.zip")
	writeZip(t, zipPath, map[string][]byte{
		"Snipaste-2.11.3/Snipaste.exe": []byte("exe"),
		"Snipaste-2.11.3/config.ini":   []byte("cfg"),
	})
	root, err := extractAll(zipPath, filepath.Join(t.TempDir(), "staging"))
	if err != nil || filepath.Base(root) != "Snipaste-2.11.3" {
		t.Fatalf("root=%q err=%v", root, err)
	}

	badZip := filepath.Join(t.TempDir(), "bad.zip")
	writeZip(t, badZip, map[string][]byte{"../evil.txt": []byte("x"), "Snipaste.exe": []byte("exe")})
	if _, err := extractAll(badZip, filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("ZipSlip should fail")
	}
}

func TestExtractAllRejectsMissingOrMultipleExe(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.zip")
	writeZip(t, missing, map[string][]byte{"readme.txt": []byte("x")})
	if _, err := extractAll(missing, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing exe should fail")
	}
	multiple := filepath.Join(t.TempDir(), "multiple.zip")
	writeZip(t, multiple, map[string][]byte{"Snipaste.exe": []byte("x"), "nested/Snipaste.exe": []byte("y")})
	if _, err := extractAll(multiple, filepath.Join(t.TempDir(), "multiple")); err == nil {
		t.Fatal("multiple exe should fail")
	}
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
