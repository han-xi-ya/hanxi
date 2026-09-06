package releases

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeReleases(t *testing.T) {
	src := []githubRelease{
		{TagName: "2.9.10", PublishedAt: "2026-08-01T00:00:00Z", HTMLURL: "https://github.com/microsoft/WSL/releases/tag/2.9.10",
			Assets: []githubAsset{
				{Name: "wsl.2.9.10.0.x64.msi", Size: 17 << 20, APIURL: "https://github.com/dl/x64.msi"},
				{Name: "wsl.2.9.10.0.arm64.msi", Size: 15 << 20, APIURL: "https://github.com/dl/arm.msi"},
				{Name: "wsl.appx", Size: 9, APIURL: "https://github.com/dl/appx"},
			}},
		{TagName: "2.8.0", Draft: true, Assets: []githubAsset{{Name: "wsl.2.8.0.0.x64.msi"}}},
		{TagName: "0.0.1-source-only", Assets: []githubAsset{{Name: "source.zip"}}},
		{TagName: "2.7.1", Prerelease: true, Assets: []githubAsset{{Name: "wsl.2.7.1.0.x64.msi"}}},
		{TagName: "not-a-version", Assets: []githubAsset{{Name: "wsl.x.msi"}}},
	}
	got := normalizeReleases(src)
	if len(got) != 2 {
		t.Fatalf("应保留 2.9.10 与预发布 2.7.1（剔除 draft/无MSI/坏tag），得 %+v", got)
	}
	first := got[0]
	if first.Tag != "2.9.10" || first.Published != "2026-08-01" || len(first.Assets) != 2 {
		t.Fatalf("首项字段错误: %+v", first)
	}
	if first.Assets[0].Platform != "x64" || first.Assets[1].Platform != "ARM64" {
		t.Fatalf("资产平台判定错误: %+v", first.Assets)
	}
	if !got[1].Prerelease {
		t.Fatal("预发布标记应保留展示")
	}
}

func TestLatestStableSkipsPrerelease(t *testing.T) {
	list := []Release{
		{Tag: "2.8.0", Prerelease: true},
		{Tag: "2.7.10"},
		{Tag: "2.7.9"},
	}
	if got := LatestStable(list); got != "2.7.10" {
		t.Fatalf("LatestStable = %s, want 2.7.10", got)
	}
	if got := LatestStable([]Release{{Tag: "1.0", Prerelease: true}}); got != "" {
		t.Fatalf("全预发布应为空串，得 %s", got)
	}
}

func TestOverviewFromRelations(t *testing.T) {
	snap := ReleaseSnapshot{
		Releases:  []Release{{Tag: "2.9.10", Assets: []Asset{{Name: "wsl.2.9.10.0.x64.msi"}}}},
		FetchedAt: time.Now(),
	}
	cases := []struct {
		name     string
		local    string
		wantRel  string
		wantLate string
	}{
		{"未安装", "", "unknown", "2.9.10"},
		{"可更新", "2.7.13", "update", "2.9.10"},
		{"已最新", "2.9.10", "latest", "2.9.10"},
		{"本机更新", "2.9.20", "ahead", "2.9.10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ov := overviewFrom(snap, c.local)
			if ov.Relation != c.wantRel || ov.Latest != c.wantLate || ov.LocalVersion != c.local {
				t.Fatalf("relation/latest/local = %s/%s/%s", ov.Relation, ov.Latest, ov.LocalVersion)
			}
			if ov.RelationDetail == "" {
				t.Fatal("关系说明文案不能为空")
			}
		})
	}
}

func TestCacheServesStaleOnError(t *testing.T) {
	// 双源皆必然失败（API 不可解析域名 + 无 Atom 端点）：缓存过期时降级 stale 而非报错。
	failSrc := source{client: &http.Client{Timeout: 500 * time.Millisecond}, endpoint: "https://nonexistent.invalid/releases"}
	c := newCache(failSrc)
	c.now = func() time.Time { return time.Unix(0, 0).Add(24 * time.Hour) }
	c.data = []Release{{Tag: "2.5.0", Assets: []Asset{{Name: "wsl.2.5.0.0.x64.msi"}}}}
	c.fetchedAt = time.Unix(0, 0) // 已远超 TTL
	snap, err := c.get()
	if err != nil {
		t.Fatalf("网络失败但有旧缓存不应报错: %v", err)
	}
	if !snap.IsStale || len(snap.Releases) != 1 {
		t.Fatalf("应返回 stale=true 的旧缓存: %+v", snap)
	}
}

const sampleAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <updated>2026-08-05T00:00:00Z</updated>
    <published>2026-08-01T09:00:00Z</published>
    <link rel="edit" href="https://api.github.com/repos/microsoft/WSL/releases/111"/>
    <link rel="alternate" href="https://github.com/microsoft/WSL/releases/tag/2.9.10"/>
  </entry>
  <entry>
    <published>2026-07-02T09:00:00Z</published>
    <link rel="alternate" href="https://github.com/microsoft/WSL/releases/tag/v2.8.0"/>
  </entry>
  <entry>
    <published>bad</published>
    <link rel="alternate" href="https://github.com/microsoft/WSL/releases/tag/not-a-version"/>
  </entry>
</feed>`

func TestFetchAtomFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.WriteHeader(http.StatusForbidden) // 模拟 api.github.com 区域性 403
		case "/atom":
			_, _ = w.Write([]byte(sampleAtom))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// fetchAtom 直连解析：tag 归一、坏值剔除、四段资产名合成。
	src := source{client: srv.Client(), atomEndpoint: srv.URL + "/atom"}
	list, err := src.fetchAtom()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应解析出 2 个合法 tag: %+v", list)
	}
	if list[0].Tag != "2.9.10" || list[0].Published != "2026-08-01" {
		t.Fatalf("首项字段错误: %+v", list[0])
	}
	if list[1].Tag != "2.8.0" { // v 前缀归一
		t.Fatalf("v 前缀未归一: %+v", list[1])
	}
	x64 := list[0].Assets[0]
	if x64.Name != "wsl.2.9.10.0.x64.msi" || x64.Size != 0 || !strings.HasSuffix(x64.URL, "/wsl.2.9.10.0.x64.msi") {
		t.Fatalf("x64 资产合成错误: %+v", x64)
	}
	if list[1].Assets[0].Name != "wsl.2.8.0.0.x64.msi" {
		t.Fatalf("四段补齐命名错误: %s", list[1].Assets[0].Name)
	}

	// fetchList 双源链：API 403 → Atom 救活并标记 fallback。
	src.endpoint = srv.URL + "/api"
	got, fallback, err := src.fetchList()
	if err != nil || !fallback || len(got) != 2 {
		t.Fatalf("降级链失守: len=%d fallback=%v err=%v", len(got), fallback, err)
	}
}

func TestMsiAssetNamePadding(t *testing.T) {
	cases := map[string]string{"2.9.10": "wsl.2.9.10.0.x64.msi", "2.8": "wsl.2.8.0.0.x64.msi", "2.7.13.0": "wsl.2.7.13.0.arm64.msi"}
	for tag, want := range cases {
		arch := "x64"
		if strings.Contains(want, "arm64") {
			arch = "arm64"
		}
		if got := msiAssetName(tag, arch); got != want {
			t.Fatalf("msiAssetName(%q,%s)=%q want %q", tag, arch, got, want)
		}
	}
}
