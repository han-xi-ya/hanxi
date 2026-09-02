package goversion

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	got := normalize([]goRelease{
		{Version: "go1.26.7", Stable: true},
		{Version: "go1.27rc1", Stable: false},
		{Version: "go1.27.0", Stable: true},
		{Version: "go1.27.1", Stable: true},
		{Version: "go1.26.8", Stable: true},
		{Version: "go1.25.9", Stable: true},
		{Version: "go1.27.1", Stable: true},
	})
	if len(got) != 2 {
		t.Fatalf("channels=%#v", got)
	}
	if got[0].Key != "stable" || got[0].Releases[0].Version != "1.27.1" {
		t.Fatalf("stable=%#v", got[0])
	}
	if got[1].Key != "oldstable" || got[1].Releases[0].Version != "1.26.8" {
		t.Fatalf("oldstable=%#v", got[1])
	}
}

func TestSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"version":"go1.27.1","stable":true},
			{"version":"go1.26.8","stable":true}
		]`))
	}))
	defer server.Close()
	channels, err := (source{client: server.Client(), endpoint: server.URL}).fetch()
	if err != nil || len(channels) != 2 {
		t.Fatalf("channels=%#v err=%v", channels, err)
	}
}

func TestSourceErrors(t *testing.T) {
	for _, body := range []string{"{", `[{"version":"go1.28rc1","stable":false}]`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		if _, err := (source{client: server.Client(), endpoint: server.URL}).fetch(); err == nil {
			server.Close()
			t.Fatalf("expected error for %q", body)
		}
		server.Close()
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxBody+1)))
	}))
	defer server.Close()
	if _, err := (source{client: server.Client(), endpoint: server.URL}).fetch(); err == nil {
		t.Fatal("expected body limit error")
	}
}
