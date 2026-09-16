package gitversion

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

func TestNormalizeReleases(t *testing.T) {
	input := []githubRelease{
		{TagName: "v2.49.0.windows.1", PublishedAt: "2025-03-14T12:00:00Z"},
		{TagName: "v2.51.0.windows.1", PublishedAt: "2025-08-18T12:00:00Z"},
		{TagName: "v2.50.0.windows.2", PublishedAt: "2025-07-01T12:00:00Z"},
		{TagName: "v2.50.0.windows.1", PublishedAt: "2025-06-01T12:00:00Z"},
		{TagName: "v2.48.0.windows.1"},
		{TagName: "v2.47.0.windows.1"},
		{TagName: "v9.0.0.windows.1", Draft: true},
		{TagName: "v8.0.0.windows.1", Prerelease: true},
		{TagName: "v2.51.0-rc1.windows.1"},
		{TagName: "v2.51.0"},
		{TagName: "v2.51.0.windows.1"},
	}
	got := normalizeReleases(input)
	if len(got) != maxReleaseCount {
		t.Fatalf("len = %d, want %d: %#v", len(got), maxReleaseCount, got)
	}
	want := []string{"2.51.0.windows.1", "2.50.0.windows.2", "2.50.0.windows.1", "2.49.0.windows.1", "2.48.0.windows.1"}
	for i := range want {
		if got[i].Version != want[i] {
			t.Fatalf("release[%d] = %q, want %q", i, got[i].Version, want[i])
		}
	}
	if got[0].Published != "2025-08-18" {
		t.Fatalf("published = %q", got[0].Published)
	}
}

func TestRemoteSourceFetchRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
			t.Error("missing GitHub headers")
		}
		_, _ = w.Write([]byte(`[
			{"tag_name":"v2.50.0.windows.1","published_at":"2025-06-01T00:00:00Z"},
			{"tag_name":"v2.51.0.windows.1","published_at":"2025-08-18T00:00:00Z"},
			{"tag_name":"v2.52.0.windows.1","prerelease":true}
		]`))
	}))
	defer server.Close()

	list, err := (remoteSource{client: server.Client(), endpoint: server.URL}).fetchRemote()
	if err != nil || len(list) != 2 || list[0].Version != "2.51.0.windows.1" {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}

func TestRemoteSourceErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "non-200", code: http.StatusForbidden, body: `{}`},
		{name: "invalid-json", code: http.StatusOK, body: `{`},
		{name: "oversized", code: http.StatusOK, body: strings.Repeat("x", maxResponseBody+1)},
		{name: "empty-list", code: http.StatusOK, body: `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			if _, err := (remoteSource{client: server.Client(), endpoint: server.URL}).fetchRemote(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestCachedReleasesFreshAndClone 真实 remoteversion.Cache：新鲜命中只发一次请求，
// 且返回值深拷贝隔离（调用方改写不污染缓存）。TTL 到期与 stale-if-error 属公共缓存
// 本体语义，由 remoteversion 包测试钉死（见其 TestCacheFreshStaleAndClone）。
func TestCachedReleasesFreshAndClone(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"tag_name":"v2.51.0.windows.1"}]`))
	}))
	defer server.Close()

	cache := remoteversion.NewCache(remoteSource{client: server.Client(), endpoint: server.URL}.fetchRemote, cloneReleases)
	first, err := cachedReleases(cache)
	if err != nil || len(first) != 1 || first[0].Stale {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	first[0].Stale = true
	second, err := cachedReleases(cache)
	if err != nil || second[0].Stale || calls.Load() != 1 {
		t.Fatalf("second=%#v calls=%d err=%v", second, calls.Load(), err)
	}
}

// fakeReleaseGetter 注入假缓存结果，覆盖 stale 标记与错误透传两条支路
// （真实 Cache 的 now 时钟注入在包外不可达，原 newReleaseCache+now 覆盖的
// stale 断言迁移至此，断言语义不变）。
type fakeReleaseGetter struct {
	data  []Release
	stale bool
	err   error
}

func (f fakeReleaseGetter) Get() ([]Release, bool, time.Time, error) {
	return cloneReleases(f.data), f.stale, time.Time{}, f.err
}

func TestCachedReleasesStaleAndError(t *testing.T) {
	stale, err := cachedReleases(fakeReleaseGetter{data: []Release{{Version: "2.51.0.windows.1"}}, stale: true})
	if err != nil || len(stale) != 1 || !stale[0].Stale {
		t.Fatalf("stale=%#v err=%v", stale, err)
	}
	if _, err := cachedReleases(fakeReleaseGetter{err: errors.New("官网版本查询失败")}); err == nil {
		t.Fatal("expected error passthrough")
	}
}

func TestReleaseCacheConcurrentFetchOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"tag_name":"v2.51.0.windows.1"}]`))
	}))
	defer server.Close()
	cache := remoteversion.NewCache(remoteSource{client: server.Client(), endpoint: server.URL}.fetchRemote, cloneReleases)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := cachedReleases(cache); err != nil {
				t.Errorf("get: %v", err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestReleaseCacheInitialFailure(t *testing.T) {
	cache := remoteversion.NewCache(
		remoteSource{client: &http.Client{Timeout: 50 * time.Millisecond}, endpoint: "http://127.0.0.1:1"}.fetchRemote,
		cloneReleases)
	if _, err := cachedReleases(cache); err == nil {
		t.Fatal("expected initial failure")
	}
}

// TestOfficialRedirectPolicy 官网重定向白名单语义（自建 checkGitHubRedirect 已收口至
// remoteversion）：非 https 或非 api.github.com 一律拒绝，官方 https 放行。
func TestOfficialRedirectPolicy(t *testing.T) {
	allowed := map[string]struct{}{"api.github.com": {}}
	for _, raw := range []string{"http://api.github.com/path", "https://evil.example/path"} {
		u, _ := url.Parse(raw)
		if err := remoteversion.ValidateURL(u, allowed); err == nil {
			t.Fatalf("expected redirect rejection for %s", raw)
		}
	}
	u, _ := url.Parse("https://api.github.com/path")
	if err := remoteversion.ValidateURL(u, allowed); err != nil {
		t.Fatalf("official redirect: %v", err)
	}
	// 端到端：默认官网客户端遇跨主机重定向必须失败（302 触发 CheckRedirect 复验）
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://evil.example/steal")
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()
	if _, err := defaultRemoteSource().client.Get(redirector.URL); err == nil {
		t.Fatal("expected redirect rejection via client")
	}
}

func ExampleDownloadPageURL() {
	fmt.Println(DownloadPageURL())
	// Output: https://git-scm.com/download/win
}
