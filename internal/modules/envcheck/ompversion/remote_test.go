package ompversion

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer 起一次性假官网（httptest 为 http，主机白名单不适用于
// defaultSource 的真 client，fetch 缝直接注入 server.Client() 绕开）。
func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNormalize(t *testing.T) {
	got := normalize([]githubRelease{
		{TagName: "v18.16.0", Published: "2026-11-02T19:49:20Z"},
		{TagName: "v18.15.0", Published: "2026-11-01T14:17:08Z"},
		{TagName: "18.14.2"}, // 无 v 前缀 tag 也认
		{TagName: "v18.16.0", Published: "2026-11-02T19:49:20Z"}, // 重复去重
		{TagName: "v20.0.0-canary.3", Prerelease: true},          // canary 预发布出局
		{TagName: "v19.0.0", Draft: true},                        // draft 出局
		{TagName: "omp-nightly"},                                 // 非法 tag 出局
		{TagName: "v18.14.1"},
		{TagName: "v18.14.0"},
	})
	if len(got) != 1 {
		t.Fatalf("channels=%#v", got)
	}
	ch := got[0]
	if ch.Key != "stable" || len(ch.Releases) != maxReleaseCount {
		t.Fatalf("channel=%#v", ch)
	}
	want := []string{"18.16.0", "18.15.0", "18.14.2"}
	for i, version := range want {
		if ch.Releases[i].Version != version {
			t.Fatalf("releases=%#v want head %v", ch.Releases, want)
		}
	}
	if ch.Releases[0].Published != "2026-11-02" {
		t.Fatalf("published=%q", ch.Releases[0].Published)
	}
	if normalize([]githubRelease{{TagName: "v1.2.3-rc.1"}}) != nil {
		t.Fatal("all-prerelease input should yield nil")
	}
}

func TestNormalizePublishedBadTime(t *testing.T) {
	got := normalize([]githubRelease{{TagName: "v1.2.3", Published: "not-a-time"}})
	if len(got) != 1 || got[0].Releases[0].Published != "" {
		t.Fatalf("bad timestamp should degrade to empty: %#v", got)
	}
}

func TestSourceFetch(t *testing.T) {
	server := newTestServer(t, `[
		{"tag_name":"v18.16.0","draft":false,"prerelease":false,"published_at":"2026-11-02T19:49:20Z","assets_url":"https://api.github.com/x"},
		{"tag_name":"v18.15.0","draft":false,"prerelease":false,"published_at":"2026-11-01T14:17:08Z"}
	]`)
	channels, err := (source{client: server.Client(), endpoint: server.URL}).fetch()
	if err != nil || len(channels) != 1 || channels[0].Releases[0].Version != "18.16.0" {
		t.Fatalf("channels=%#v err=%v", channels, err)
	}
}

func TestSourceErrors(t *testing.T) {
	for _, body := range []string{"{", `[{"tag_name":"v9.0.0-canary","prerelease":true}]`} {
		server := newTestServer(t, body)
		if _, err := (source{client: server.Client(), endpoint: server.URL}).fetch(); err == nil {
			t.Fatalf("expected error for %q", body)
		}
	}
}
