package javaversion

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

const validResponse = `[{"release_name":"jdk-21.0.4+7","release_type":"ga","timestamp":"2025-07-16T10:00:00Z","vendor":"eclipse","version_data":{"major":21,"openjdk_version":"21.0.4+7-LTS","optional":"LTS"},"binaries":[{"architecture":"x64","image_type":"jdk","jvm_impl":"hotspot","os":"windows","project":"jdk"}]}]`

func TestFetchFeatureContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") == "" {
			t.Fatal("missing HTTP headers")
		}
		q := r.URL.Query()
		for key, want := range map[string]string{"architecture": "x64", "image_type": "jdk", "jvm_impl": "hotspot", "os": "windows", "project": "jdk", "vendor": "eclipse"} {
			if q.Get(key) != want {
				t.Errorf("%s=%q, want %q", key, q.Get(key), want)
			}
		}
		_, _ = w.Write([]byte(validResponse))
	}))
	defer server.Close()

	release, ok, err := (source{client: server.Client(), baseURL: server.URL, features: []int{21}}).fetchFeature(21)
	if err != nil || !ok || normalizeVersion(release) != "21.0.4+7-LTS" {
		t.Fatalf("release=%#v ok=%v err=%v", release, ok, err)
	}
}

func TestFetchDiscoversLTSAndFeatureChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/info/available_releases":
			_, _ = w.Write([]byte(`{"most_recent_feature_release":26,"most_recent_lts":25}`))
		case strings.Contains(r.URL.Path, "/25/ga"):
			_, _ = w.Write([]byte(strings.ReplaceAll(validResponse, `21`, `25`)))
		case strings.Contains(r.URL.Path, "/26/ga"):
			body := strings.ReplaceAll(validResponse, `21`, `26`)
			body = strings.Replace(body, `"optional":"LTS"`, `"optional":""`, 1)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	channels, err := (source{client: server.Client(), baseURL: server.URL}).fetch()
	if err != nil || len(channels) != 2 {
		t.Fatalf("channels=%#v err=%v", channels, err)
	}
	if channels[0].Key != "26" || channels[0].Detail != "Feature · Temurin JDK HotSpot GA" || channels[1].Key != "25" || channels[1].Detail != "LTS · Temurin JDK HotSpot GA" {
		t.Fatalf("channels=%#v", channels)
	}
}

func TestFetchNormalizesChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validResponse)) }))
	defer server.Close()
	channels, err := (source{client: server.Client(), baseURL: server.URL, features: []int{21}}).fetch()
	if err != nil || len(channels) != 1 {
		t.Fatalf("channels=%#v err=%v", channels, err)
	}
	got := channels[0]
	if got.Key != "21" || got.Detail != "LTS · Temurin JDK HotSpot GA" || got.Releases[0].Published != "2025-07-16" {
		t.Fatalf("channel=%#v", got)
	}
}

func TestFetchRejectsUnexpectedAssets(t *testing.T) {
	body := strings.Replace(validResponse, `"vendor":"eclipse"`, `"vendor":"oracle"`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	_, err := (source{client: server.Client(), baseURL: server.URL, features: []int{21}}).fetch()
	if err == nil {
		t.Fatal("expected no valid release error")
	}
}

func TestFetchErrors(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		code       int
	}{
		{"status", `{}`, http.StatusForbidden}, {"json", `{`, http.StatusOK}, {"oversized", strings.Repeat("x", maxBody+1), http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.code); _, _ = w.Write([]byte(tt.body)) }))
			defer server.Close()
			_, _, err := (source{client: server.Client(), baseURL: server.URL}).fetchFeature(21)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFeatureURLUsesEncodedQuery(t *testing.T) {
	raw, err := featureURL(apiBaseURL, 21)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.Scheme != "https" || u.Hostname() != "api.adoptium.net" || u.Query().Get("vendor") != "eclipse" {
		t.Fatalf("url=%s", raw)
	}
}

func TestRelationForVendorAware(t *testing.T) {
	temurin := detect.ToolInfo{Version: "21.0.3+1", Status: detect.StatusInstalled, Details: &detect.ToolDetails{Java: &detect.JavaDetails{Vendor: TemurinVendor}}}
	if got := RelationFor(temurin, "21.0.4+7"); got != remoteversion.RelationUpdateAvailable {
		t.Fatalf("temurin relation=%s", got)
	}
	other := temurin
	other.Details = &detect.ToolDetails{Java: &detect.JavaDetails{Vendor: "Amazon Corretto"}}
	if got := RelationFor(other, "21.0.4+7"); got != remoteversion.RelationUnknown {
		t.Fatalf("other relation=%s", got)
	}
	other.Version = "17.0.12"
	if got := RelationFor(other, "21.0.4+7"); got != remoteversion.RelationUpdateAvailable {
		t.Fatalf("feature relation=%s", got)
	}
	if got := RelationFor(detect.ToolInfo{Status: detect.StatusMissing}, "21"); got != remoteversion.RelationNotInstalled {
		t.Fatalf("missing relation=%s", got)
	}
}
