package remoteversion

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheFreshStaleAndClone(t *testing.T) {
	var calls atomic.Int32
	fail := false
	cache := NewCache(func() ([]Release, error) {
		calls.Add(1)
		if fail {
			return nil, errors.New("offline")
		}
		return []Release{{Version: "1.2.3"}}, nil
	}, func(src []Release) []Release { return append([]Release(nil), src...) })
	now := time.Now()
	cache.now = func() time.Time { return now }

	first, stale, _, err := cache.Get()
	if err != nil || stale || len(first) != 1 {
		t.Fatalf("first=%#v stale=%v err=%v", first, stale, err)
	}
	first[0].Version = "polluted"
	second, stale, _, err := cache.Get()
	if err != nil || stale || second[0].Version != "1.2.3" || calls.Load() != 1 {
		t.Fatalf("second=%#v stale=%v calls=%d err=%v", second, stale, calls.Load(), err)
	}
	fail = true
	now = now.Add(2 * CacheTTL)
	third, stale, _, err := cache.Get()
	if err != nil || !stale || third[0].Version != "1.2.3" {
		t.Fatalf("third=%#v stale=%v err=%v", third, stale, err)
	}
}

func TestCacheConcurrentFetchOnce(t *testing.T) {
	var calls atomic.Int32
	cache := NewCache(func() (int, error) {
		calls.Add(1)
		return 1, nil
	}, func(v int) int { return v })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _, _, _ = cache.Get() })
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestFetchLimitsAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("ok"))
		case "/large":
			_, _ = w.Write(make([]byte, 11))
		default:
			http.Error(w, "no", http.StatusBadGateway)
		}
	}))
	defer server.Close()
	if body, err := Fetch(server.Client(), server.URL+"/ok", 10, nil); err != nil || string(body) != "ok" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if _, err := Fetch(server.Client(), server.URL+"/large", 10, nil); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := Fetch(server.Client(), server.URL+"/bad", 10, nil); err == nil {
		t.Fatal("expected status error")
	}
}

func TestValidateURL(t *testing.T) {
	hosts := map[string]struct{}{"go.dev": {}}
	for _, raw := range []string{"http://go.dev/dl", "https://evil.example/dl"} {
		u, _ := url.Parse(raw)
		if err := ValidateURL(u, hosts); err == nil {
			t.Fatalf("expected rejection: %s", raw)
		}
	}
	u, _ := url.Parse("https://go.dev/dl")
	if err := ValidateURL(u, hosts); err != nil {
		t.Fatal(err)
	}
}
