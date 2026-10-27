package version

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReleasesBodyStableOnly(t *testing.T) {
	body, _ := json.Marshal([]release{
		{TagName: "6.5.1800.0", PublishedAt: "2026-08-06T00:00:00Z", Assets: []asset{{Name: "NanaZip_6.5.1800.0.msixbundle", URL: "https://example.test/stable", Size: 12, Digest: "sha256:" + repeatHex("a")}}},
		{TagName: "7.0.1800.0", Prerelease: true, Assets: []asset{{Name: "NanaZipPreview_7.0.1800.0.msixbundle", Size: 12, Digest: "sha256:" + repeatHex("b")}}},
		{TagName: "6.4.0", Assets: []asset{{Name: "NanaZip_6.4.0.msixbundle", Size: 12, Digest: "sha256:" + repeatHex("c")}}},
	})
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Version != "6.5.1800.0" {
		t.Fatalf("unexpected releases: %#v", list)
	}
}

func TestInspectBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "NanaZip_6.5.1800.0.msixbundle")
	if err := writeTestBundle(path, "6.5.1800.0", currentTestArchitecture()); err != nil {
		t.Fatal(err)
	}
	architectures, err := inspectBundle(path, "6.5.1800.0")
	if err != nil {
		t.Fatalf("inspectBundle failed: %v", err)
	}
	if len(architectures) != 1 || architectures[0] != currentTestArchitecture() {
		t.Fatalf("unexpected architectures: %#v", architectures)
	}
	if _, err := inspectBundle(path, "6.4.0.0"); err == nil {
		t.Fatal("expected version mismatch")
	}
}

func writeTestBundle(path, version, architecture string) error {
	inner := new(bytes.Buffer)
	innerZip := zip.NewWriter(inner)
	manifest, err := innerZip.Create("AppxManifest.xml")
	if err != nil {
		return err
	}
	_, err = manifest.Write([]byte(`<?xml version="1.0"?><Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"><Identity Name="` + nanaZipName + `" Publisher="` + nanaZipPublisher + `" Version="` + version + `" ProcessorArchitecture="` + architecture + `"/><Applications><Application Id="` + nanaZipAppID + `"/></Applications></Package>`))
	if err != nil {
		return err
	}
	if err := innerZip.Close(); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	outer := zip.NewWriter(file)
	bundleManifest, err := outer.Create("AppxMetadata/AppxBundleManifest.xml")
	if err != nil {
		return err
	}
	_, err = bundleManifest.Write([]byte(`<?xml version="1.0"?><Bundle xmlns="http://schemas.microsoft.com/appx/2013/bundle"><Identity Name="` + nanaZipName + `" Publisher="` + nanaZipPublisher + `" Version="` + version + `"/><Packages><Package Type="application" Architecture="` + architecture + `" FileName="NanaZipPackage_` + version + `_` + architecture + `.msix"/></Packages></Bundle>`))
	if err != nil {
		return err
	}
	packageFile, err := outer.Create("NanaZipPackage_" + version + "_" + architecture + ".msix")
	if err != nil {
		return err
	}
	if _, err := packageFile.Write(inner.Bytes()); err != nil {
		return err
	}
	if err := outer.Close(); err != nil {
		return err
	}
	return file.Close()
}

func currentTestArchitecture() string {
	if isCompatibleArchitecture("x64") {
		return "x64"
	}
	if isCompatibleArchitecture("arm64") {
		return "arm64"
	}
	return "x86"
}

func repeatHex(value string) string {
	var buffer bytes.Buffer
	for range 64 {
		buffer.WriteString(value)
	}
	return buffer.String()
}

// --- fetchReleases 限幅与截断识别（与 bundle.go readZipEntry 同型纪律）---

// releasesSample 恰好 len 字节的合法 releases 响应体：直接用作"正好等于上限"
// 边界样本（测试接缝把 maxFeedBytes 压到它的长度）。
var releasesSample = `[{"tag_name":"6.5.1800.0","published_at":"2026-08-06T00:00:00Z","assets":[{"name":"NanaZip_6.5.1800.0.msixbundle","browser_download_url":"https://example.test/n.msixbundle","size":12,"digest":"sha256:` + repeatHex("a") + `"}]}]`

// withMaxFeedBytes 临时压低读取上限（测试接缝，见 remote.go maxFeedBytes 注释）。
func withMaxFeedBytes(t *testing.T, n int) {
	t.Helper()
	old := maxFeedBytes
	maxFeedBytes = n
	t.Cleanup(func() { maxFeedBytes = old })
}

// withMirrors 临时把 apiBaseURLs 指到测试镜像，用例结束还原。
func withMirrors(t *testing.T, urls ...string) {
	t.Helper()
	old := apiBaseURLs
	apiBaseURLs = urls
	t.Cleanup(func() { apiBaseURLs = old })
}

// serveReleasesMirror 起一个单镜像测试服务器（status=0 表示隐式 200），
// 并把 apiBaseURLs 指过去。fetchReleases 请求的路径由实现固定，这里不作断言。
func serveReleasesMirror(t *testing.T, status int, body []byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withMirrors(t, srv.URL)
}

// padJSONTo 把以 '[' 开头的 JSON 体在首个记号后补白到恰好 n 字节（数组内空白合法）。
func padJSONTo(t *testing.T, content string, n int) []byte {
	t.Helper()
	if n < len(content) || content[0] != '[' {
		t.Fatalf("无法把 %d 字节的体补到 %d 字节", len(content), n)
	}
	return []byte("[" + strings.Repeat(" ", n-len(content)) + content[1:])
}

func TestFetchReleasesCapped(t *testing.T) {
	limit := len(releasesSample)
	withMaxFeedBytes(t, limit)

	// 小于上限：正常返回
	serveReleasesMirror(t, 0, padJSONTo(t, "[]", limit-16))
	body, err := fetchReleases()
	if err != nil {
		t.Fatalf("小于上限应正常返回: %v", err)
	}
	if len(body) != limit-16 {
		t.Errorf("响应体字节数应完整返回: %d", len(body))
	}

	// 正好等于上限：不得报超限，且响应完整可解析
	serveReleasesMirror(t, 0, []byte(releasesSample))
	body, err = fetchReleases()
	if err != nil {
		t.Fatalf("正好等于上限不得报超限: %v", err)
	}
	if len(body) != limit {
		t.Errorf("响应体字节数应为 %d，实际 %d", limit, len(body))
	}
	list, err := parseReleasesBody(body)
	if err != nil || len(list) != 1 || list[0].Version != "6.5.1800.0" {
		t.Errorf("正好等于上限的响应应完整可用: %v %+v", err, list)
	}

	// 超过上限 1 字节：报超限哨兵，且截断响应绝不伪装成解析失败往下走
	serveReleasesMirror(t, 0, append([]byte(releasesSample), ' '))
	_, err = fetchReleases()
	if err == nil {
		t.Fatal("超过上限必须报错")
	}
	if !errors.Is(err, errResponseTooLarge) {
		t.Errorf("应命中超限哨兵: %v", err)
	}
	if strings.Contains(err.Error(), "parse releases") {
		t.Errorf("超限响应被交给了解析层（伪装成格式问题）: %v", err)
	}
}

func TestFetchReleasesStatusHonesty(t *testing.T) {
	// 5xx + 合法 JSON 体：闸门须报状态码，而非放行响应体或误报超限
	serveReleasesMirror(t, http.StatusServiceUnavailable, []byte(releasesSample))
	_, err := fetchReleases()
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("非 200 应报真实状态码: %v", err)
	}
	if errors.Is(err, errResponseTooLarge) {
		t.Errorf("5xx 不应被归类成超限: %v", err)
	}
}

func TestFetchReleasesReadError(t *testing.T) {
	// 声明 Content-Length 后连接提前掐断：读错误原样上报
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 512\r\n\r\n[{\"tag_name\":"))
		_ = conn.Close()
	}))
	srv.Start()
	t.Cleanup(srv.Close)
	withMirrors(t, srv.URL)

	_, err := fetchReleases()
	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Errorf("读错误应原样上报（带 EOF 痕迹）: %v", err)
	}
}
