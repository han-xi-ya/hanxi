package dotnetversion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

var fixtureNow = time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)

// validIndex 为 2026-09-03 builds.dotnet.microsoft.com 真实 releases-index 的浓缩样本
// （11.0 预览线无 eol-date 且 latest-release 带 -preview 后缀，均为实机数据形态）。
const validIndex = `{"releases-index": [
	{"channel-version": "11.0", "latest-release": "11.0.0-preview.7", "latest-release-date": "2026-08-11", "support-phase": "preview", "release-type": "sts", "latest-runtime": "11.0.0-preview.7.26381.103", "latest-sdk": "11.0.100-preview.7.26381.103"},
	{"channel-version": "10.0", "latest-release": "10.0.11", "latest-release-date": "2026-08-11", "support-phase": "active", "release-type": "lts", "latest-runtime": "10.0.11", "latest-sdk": "10.0.400", "eol-date": "2028-11-14"},
	{"channel-version": "9.0", "latest-release": "9.0.19", "latest-release-date": "2026-08-11", "support-phase": "maintenance", "release-type": "sts", "latest-runtime": "9.0.19", "latest-sdk": "9.0.317", "eol-date": "2026-11-10"},
	{"channel-version": "8.0", "latest-release": "8.0.30", "latest-release-date": "2026-08-11", "support-phase": "maintenance", "release-type": "lts", "latest-runtime": "8.0.30", "latest-sdk": "8.0.424", "eol-date": "2026-11-10"},
	{"channel-version": "7.0", "latest-release": "7.0.20", "latest-release-date": "2024-05-28", "support-phase": "eol", "release-type": "sts", "latest-runtime": "7.0.20", "latest-sdk": "7.0.410", "eol-date": "2024-05-14"},
	{"channel-version": "6.0", "latest-release": "6.0.36", "latest-release-date": "2024-11-12", "support-phase": "maintenance", "release-type": "lts", "latest-runtime": "6.0.36", "latest-sdk": "6.0.428", "eol-date": "2024-11-12"}
]}`

func decodeIndex(t *testing.T, body string) releasesIndex {
	t.Helper()
	var index releasesIndex
	if err := json.Unmarshal([]byte(body), &index); err != nil {
		t.Fatalf("fixture json: %v", err)
	}
	return index
}

func TestNormalizeHappyPath(t *testing.T) {
	channels, err := normalize(decodeIndex(t, validIndex), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	// 11.0 preview 与 7.0 eol、6.0 maintenance 但 eol-date 已过期，均被过滤。
	if len(channels) != 3 {
		t.Fatalf("channels=%d", len(channels))
	}
	if channels[0].Key != "dotnet-10.0" || channels[1].Key != "dotnet-9.0" || channels[2].Key != "dotnet-8.0" {
		t.Fatalf("order=%s %s %s", channels[0].Key, channels[1].Key, channels[2].Key)
	}
	if channels[0].Label != ".NET 10.0" || channels[0].Releases[0].Version != "10.0.11" || channels[0].Releases[0].Published != "2026-08-11" {
		t.Fatalf("latest channel=%#v", channels[0])
	}
	if channels[0].Detail != "LTS · 活跃支持 · SDK 10.0.400 · 支持至 2028-11-14" {
		t.Fatalf("detail=%q", channels[0].Detail)
	}
	if channels[2].Detail != "LTS · 维护支持 · SDK 8.0.424 · 支持至 2026-11-10" {
		t.Fatalf("8.0 detail=%q", channels[2].Detail)
	}
}

func TestNormalizeSkipsPreviewWithoutEOLDate(t *testing.T) {
	index := decodeIndex(t, `{"releases-index": [
		{"channel-version": "11.0", "latest-release": "11.0.0-preview.7", "support-phase": "preview", "release-type": "sts", "latest-runtime": "11.0.0-preview.7.26381.103"},
		{"channel-version": "10.0", "latest-release": "10.0.1", "support-phase": "active", "release-type": "lts", "latest-runtime": "10.0.1", "eol-date": "2028-11-14"}
	]}`)
	channels, err := normalize(index, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Key != "dotnet-10.0" {
		t.Fatalf("channels=%#v", channels)
	}
}

func TestNormalizeFallbackToLatestRelease(t *testing.T) {
	index := decodeIndex(t, `{"releases-index": [
		{"channel-version": "5.0", "latest-release": "5.0.17", "support-phase": "active", "release-type": "sts", "eol-date": "2099-05-10"}
	]}`)
	channels, err := normalize(index, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	if channels[0].Releases[0].Version != "5.0.17" {
		t.Fatalf("version=%q", channels[0].Releases[0].Version)
	}
}

func TestNormalizeContractDriftFailsLoud(t *testing.T) {
	for _, body := range []string{
		`{"releases-index": []}`,
		`{"releases-index": [{"channel-version": "8", "latest-runtime": "8.0.16", "support-phase": "active"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16-preview.1", "support-phase": "active", "release-type": "lts"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "lts", "release-type": "lts"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "", "release-type": "lts"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "active", "release-type": "long-term"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "active", "release-type": "lts", "eol-date": "someday"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "active", "release-type": "lts", "latest-release-date": "not-a-date"}]}`,
		`{"releases-index": [{"channel-version": "8.0", "latest-runtime": "8.0.16", "support-phase": "active", "release-type": "lts"},{"channel-version": "8.0", "latest-runtime": "8.0.17", "support-phase": "active", "release-type": "lts"}]}`,
	} {
		if _, err := normalize(decodeIndex(t, body), fixtureNow); err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestNormalizeAllLinesExpired(t *testing.T) {
	index := decodeIndex(t, `{"releases-index": [
		{"channel-version": "6.0", "latest-runtime": "6.0.36", "support-phase": "eol", "release-type": "lts", "eol-date": "2024-11-12"}
	]}`)
	if _, err := normalize(index, fixtureNow); err == nil {
		t.Fatal("expected no-supported-line error")
	}
}

func TestSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept=%q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(validIndex))
	}))
	defer server.Close()
	s := source{client: server.Client(), endpoint: server.URL, now: fixtureNow}
	channels, err := s.fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 || channels[0].Key != "dotnet-10.0" {
		t.Fatalf("channels=%v", channels)
	}
}

func TestSourceFetchTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	s := source{client: server.Client(), endpoint: server.URL + "/index.json", now: fixtureNow}
	if _, err := s.fetch(); err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestCloneChannels(t *testing.T) {
	original := []remoteversion.Channel{
		{Key: "dotnet-10.0", Releases: []remoteversion.Release{{Version: "10.0.1"}}},
	}
	cloned := cloneChannels(original)
	cloned[0].Releases[0].Version = "polluted"
	if original[0].Releases[0].Version != "10.0.1" {
		t.Fatalf("clone polluted source: %#v", original)
	}
}
