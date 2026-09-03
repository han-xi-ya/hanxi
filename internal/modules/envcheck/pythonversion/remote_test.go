package pythonversion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const lifecycleFixture = `<!doctype html><html><body>
<section id="supported-versions">
<h2>Supported versions</h2>
<table><thead><tr><th>Branch</th><th>Schedule</th><th>Status</th><th>First release</th><th>End of life</th><th>Release manager</th></tr></thead>
<tbody>
<tr><td><p>main</p></td><td>PEP</td><td><p>feature</p></td><td><em>2027-10-06</em></td><td><em>2032-10</em></td><td>A</td></tr>
<tr><td><p>3.15</p></td><td>PEP</td><td><p>prerelease</p></td><td><em>2026-10-01</em></td><td><em>2031-10</em></td><td>B</td></tr>
<tr><td><p>3.14</p></td><td>PEP</td><td><p>bugfix</p></td><td>2025-10-07</td><td><em>2030-10</em></td><td>C</td></tr>
<tr><td><p>3.13</p></td><td>PEP</td><td><p>bugfix</p></td><td>2024-10-07</td><td><em>2029-10</em></td><td>D</td></tr>
<tr><td><p>3.12</p></td><td>PEP</td><td><p>security</p></td><td>2023-10-02</td><td><em>2028-10</em></td><td>E</td></tr>
</tbody></table>
</section>
<section id="unsupported-versions"><table><tbody><tr><td>3.11</td></tr></tbody></table></section>
</body></html>`

func TestNormalizeReleasesStrictFinalOnly(t *testing.T) {
	got := normalizeReleases([]pythonRelease{
		{Name: "Python 3.14.7", IsPublished: true, ReleaseDate: "2026-08-05T12:40:32Z"},
		{Name: "Python 3.12.14", IsPublished: true, ReleaseDate: "2026-08-12T15:23:33Z"},
		{Name: "Python 3.13.15", IsPublished: true, ReleaseDate: "2026-08-05T13:19:06Z"},
		{Name: "Python 3.14.7", IsPublished: true, ReleaseDate: "2026-08-05T12:40:32Z"},
		{Name: "Python 3.15.0rc1", IsPublished: true, PreRelease: false, ReleaseDate: "2026-09-01T00:00:00Z"},
		{Name: "Python 3.15.0", IsPublished: true, PreRelease: true, ReleaseDate: "2026-09-01T00:00:00Z"},
		{Name: "Python 3.11.14", IsPublished: false, ReleaseDate: "2026-01-01T00:00:00Z"},
		{Name: "Python 2.7.18", IsPublished: true, ReleaseDate: "2020-04-20T00:00:00Z"},
		{Name: "Python 3.14.8", IsPublished: true, ReleaseDate: "invalid"},
	})
	if len(got) != 3 {
		t.Fatalf("releases=%#v", got)
	}
	if got[0].Version != "3.14.7" || got[1].Version != "3.13.15" || got[2].Version != "3.12.14" {
		t.Fatalf("order=%#v", got)
	}
	if got[0].Published != "2026-08-05" {
		t.Fatalf("published=%q", got[0].Published)
	}
}

func TestParseLifecycles(t *testing.T) {
	got, err := parseLifecycles(lifecycleFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("lifecycles=%#v", got)
	}
	if got[0] != (Lifecycle{Minor: "3.14", Status: "bugfix", EndOfLife: "2030-10"}) {
		t.Fatalf("first=%#v", got[0])
	}
	if got[2].Status != "security" {
		t.Fatalf("last=%#v", got[2])
	}
}

func TestParseLifecyclesFailsOnContractDrift(t *testing.T) {
	for _, body := range []string{
		`<html></html>`,
		`<section id="supported-versions"><table><tbody><tr><td>3.14</td><td>bugfix</td></tr></tbody></table></section>`,
		strings.Replace(lifecycleFixture, "2030-10", "someday", 1),
		strings.Replace(lifecycleFixture, "<p>3.13</p>", "<p>3.14</p>", 1),
	} {
		if _, err := parseLifecycles(body); err == nil {
			t.Fatalf("expected strict parse error for fixture %q", body[:min(80, len(body))])
		}
	}
}

func TestBuildMinorChannels(t *testing.T) {
	catalog := Catalog{
		Releases: []remoteversion.Release{
			{Version: "3.14.7", Published: "2026-08-05"},
			{Version: "3.13.15", Published: "2026-08-05"},
			{Version: "3.12.14", Published: "2026-08-12"},
		},
		Lifecycles: []Lifecycle{
			{Minor: "3.14", Status: "bugfix", EndOfLife: "2030-10"},
			{Minor: "3.13", Status: "bugfix", EndOfLife: "2029-10"},
			{Minor: "3.12", Status: "security", EndOfLife: "2028-10"},
		},
	}
	got := BuildMinorChannels("Python 3.12.3", catalog)
	if len(got) != 1 || got[0].Key != "python-3.12" || got[0].Releases[0].Version != "3.12.14" || !strings.Contains(got[0].Detail, "Security") {
		t.Fatalf("channels=%#v", got)
	}
	for _, local := range []string{"3.15.0rc1", "3.11.9", "2.7.18", "unknown"} {
		if got := BuildMinorChannels(local, catalog); len(got) != 0 {
			t.Fatalf("BuildMinorChannels(%q)=%#v", local, got)
		}
	}
}

func TestBuildChannelsIncludesStableAndLocalLine(t *testing.T) {
	catalog := Catalog{
		Releases: []remoteversion.Release{
			{Version: "3.14.7", Published: "2026-08-05"},
			{Version: "3.12.14", Published: "2026-08-12"},
		},
		Lifecycles: []Lifecycle{{Minor: "3.12", Status: "security", EndOfLife: "2028-10"}},
	}
	got := BuildChannels("3.12.3", catalog)
	if len(got) != 2 || got[0].Key != "stable" || got[0].Releases[0].Version != "3.14.7" || got[1].Key != "python-3.12" {
		t.Fatalf("channels=%#v", got)
	}
	if got := BuildChannels("", catalog); len(got) != 1 || got[0].Key != "stable" {
		t.Fatalf("missing local channels=%#v", got)
	}
}

func TestCloneCatalog(t *testing.T) {
	original := Catalog{
		Releases:   []remoteversion.Release{{Version: "3.14.7"}},
		Lifecycles: []Lifecycle{{Minor: "3.14", Status: "bugfix"}},
	}
	cloned := cloneCatalog(original)
	cloned.Releases[0].Version = "polluted"
	cloned.Lifecycles[0].Status = "security"
	if original.Releases[0].Version != "3.14.7" || original.Lifecycles[0].Status != "bugfix" {
		t.Fatalf("clone polluted source: %#v", original)
	}
}

func TestSourceFetchFixture(t *testing.T) {
	releases, _ := json.Marshal([]pythonRelease{
		{Name: "Python 3.14.7", IsPublished: true, ReleaseDate: "2026-08-05T12:40:32Z"},
		{Name: "Python 3.13.15", IsPublished: true, ReleaseDate: "2026-08-05T13:19:06Z"},
		{Name: "Python 3.12.14", IsPublished: true, ReleaseDate: "2026-08-12T15:23:33Z"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			if r.Header.Get("Accept") != "application/json" {
				t.Errorf("release Accept=%q", r.Header.Get("Accept"))
			}
			_, _ = w.Write(releases)
		case "/lifecycle":
			if r.Header.Get("Accept") != "text/html" {
				t.Errorf("lifecycle Accept=%q", r.Header.Get("Accept"))
			}
			_, _ = w.Write([]byte(lifecycleFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := source{
		releaseClient: server.Client(), lifecycleClient: server.Client(),
		releaseEndpoint: server.URL + "/releases", lifecycleEndpoint: server.URL + "/lifecycle",
	}
	catalog, err := s.fetch()
	if err != nil || len(catalog.Releases) != 3 || len(catalog.Lifecycles) != 3 {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
}

func TestSourceFetchErrors(t *testing.T) {
	tests := []struct{ releases, lifecycle string }{
		{"{", lifecycleFixture},
		{"[]", lifecycleFixture},
		{`[{"name":"Python 3.14.7","is_published":true,"pre_release":false,"release_date":"2026-08-05T12:40:32Z"}]`, "<html></html>"},
	}
	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/releases" {
				_, _ = w.Write([]byte(tt.releases))
				return
			}
			_, _ = w.Write([]byte(tt.lifecycle))
		}))
		s := source{releaseClient: server.Client(), lifecycleClient: server.Client(), releaseEndpoint: server.URL + "/releases", lifecycleEndpoint: server.URL + "/lifecycle"}
		_, err := s.fetch()
		server.Close()
		if err == nil {
			t.Fatalf("expected error for releases=%q lifecycle=%q", tt.releases, tt.lifecycle)
		}
	}
}
