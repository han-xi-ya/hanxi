package gitversion

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestReleaseCacheFreshAndStale(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`[{"tag_name":"v2.51.0.windows.1"}]`))
			return
		}
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	now := time.Now()
	cache := newReleaseCache(remoteSource{client: server.Client(), endpoint: server.URL})
	cache.now = func() time.Time { return now }
	first, err := cache.get()
	if err != nil || len(first) != 1 || first[0].Stale {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	first[0].Stale = true
	second, err := cache.get()
	if err != nil || second[0].Stale || calls.Load() != 1 {
		t.Fatalf("second=%#v calls=%d err=%v", second, calls.Load(), err)
	}
	now = now.Add(2 * cacheTTL)
	stale, err := cache.get()
	if err != nil || !stale[0].Stale || calls.Load() != 2 {
		t.Fatalf("stale=%#v calls=%d err=%v", stale, calls.Load(), err)
	}
}

func TestReleaseCacheConcurrentFetchOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"tag_name":"v2.51.0.windows.1"}]`))
	}))
	defer server.Close()
	cache := newReleaseCache(remoteSource{client: server.Client(), endpoint: server.URL})

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := cache.get(); err != nil {
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
	cache := newReleaseCache(remoteSource{client: &http.Client{Timeout: 50 * time.Millisecond}, endpoint: "http://127.0.0.1:1"})
	if _, err := cache.get(); err == nil {
		t.Fatal("expected initial failure")
	}
}

func TestCheckGitHubRedirect(t *testing.T) {
	for _, raw := range []string{"http://api.github.com/path", "https://evil.example/path"} {
		u, _ := url.Parse(raw)
		if err := checkGitHubRedirect(&http.Request{URL: u}, nil); err == nil {
			t.Fatalf("expected redirect rejection for %s", raw)
		}
	}
	u, _ := url.Parse("https://api.github.com/path")
	if err := checkGitHubRedirect(&http.Request{URL: u}, nil); err != nil {
		t.Fatalf("official redirect: %v", err)
	}
}

func ExampleDownloadPageURL() {
	fmt.Println(DownloadPageURL())
	// Output: https://git-scm.com/download/win
}
