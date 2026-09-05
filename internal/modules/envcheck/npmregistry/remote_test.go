package npmregistry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchScopedPackage(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"name":"@anthropic-ai/claude-code","version":"2.1.261"}`))
	}))
	defer srv.Close()
	s := source{client: srv.Client(), base: srv.URL + "/", now: time.Now}
	release, err := s.fetch("@anthropic-ai/claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "2.1.261" {
		t.Fatalf("version=%q", release.Version)
	}
	if gotPath != "/@anthropic-ai%2Fclaude-code/latest" {
		t.Fatalf("escaped path=%q", gotPath)
	}
}

func TestFetchNormalizesVPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":" v1.2.3 "}`))
	}))
	defer srv.Close()
	s := source{client: srv.Client(), base: srv.URL + "/", now: time.Now}
	release, err := s.fetch("cowsay")
	if err != nil || release.Version != "1.2.3" {
		t.Fatalf("release=%#v err=%v", release, err)
	}
}

func TestFetchErrors(t *testing.T) {
	t.Run("empty-version", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"name":"x"}`))
		}))
		defer srv.Close()
		s := source{client: srv.Client(), base: srv.URL + "/", now: time.Now}
		if _, err := s.fetch("x"); err == nil {
			t.Fatal("expected error for missing version")
		}
	})
	t.Run("bad-json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()
		s := source{client: srv.Client(), base: srv.URL + "/", now: time.Now}
		if _, err := s.fetch("x"); err == nil {
			t.Fatal("expected parse error")
		}
	})
}
