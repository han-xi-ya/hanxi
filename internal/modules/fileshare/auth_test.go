package fileshare

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// BUG-001 回归套件：AuthToken 必须真正拦住未验证访客，
// 且免密模式行为逐字不变（产品定位「免密局域网共享」）。

func mustHandler(t *testing.T, server *Server) http.Handler {
	t.Helper()
	handler, err := server.handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

func loginFor(t *testing.T, handler http.Handler, token string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": token})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result()
}

func TestFileshareAuthGateBlocksAPIUntilLogin(t *testing.T) {
	server := NewServer(ShareConfig{Port: 0, SharePath: t.TempDir(), AuthToken: "s3cret"}, nil, nil)
	handler := mustHandler(t, server)

	// 页面与登录通道本身必须可达，否则访客无从输口令
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/config"},
		{http.MethodGet, "/assets/js/app.js"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(probe.method, probe.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d want 200 body=%s", probe.method, probe.path, rec.Code, rec.Body.String())
		}
	}

	// 全部数据面 API 未登录必须 401
	for _, path := range []string{"/api/list?path=", "/api/stats", "/api/download?path=x", "/api/open?path=x"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d want 401", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/upload?dir=&name=a&size=1", "/api/drop"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("x")))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST %s status=%d want 401", path, rec.Code)
		}
	}

	// /api/config 泄露位检查：只报 authRequired，不含口令
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg["authRequired"] != true {
		t.Fatalf("authRequired=%v want true", cfg["authRequired"])
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatal("config leaked auth token")
	}

	// 错误口令 → 401 且不下发 Cookie
	resp := loginFor(t, handler, "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d want 401", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("session cookie issued for wrong token")
	}
	resp.Body.Close()

	// 正确口令 → 200 + HttpOnly 会话 Cookie，携 Cookie 后数据面放行
	resp = loginFor(t, handler, "s3cret")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d want 200 body=%s", resp.StatusCode, body)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	resp.Body.Close()
	if session == nil {
		t.Fatal("no session cookie issued")
	}
	if !session.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite=%v want Lax", session.SameSite)
	}
	if !strings.Contains(session.String(), "Max-Age=") {
		t.Fatal("session cookie must carry explicit Max-Age")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/list?path=", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with session status=%d want 200 body=%s", rec.Code, rec.Body.String())
	}

	// Bearer 通道：非浏览器集成用口令直连
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats via bearer status=%d want 200", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stats with bad bearer status=%d want 401", rec.Code)
	}

	// 登录接口只接受 POST
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/login", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/login status=%d want 405", rec.Code)
	}
}

func TestFileshareAuthDisabledKeepsPasswordlessBehavior(t *testing.T) {
	server := NewServer(ShareConfig{Port: 0, SharePath: t.TempDir()}, nil, nil)
	handler := mustHandler(t, server)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/list?path=", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("passwordless list status=%d want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["authRequired"] != false {
		t.Fatalf("authRequired=%v want false", cfg["authRequired"])
	}

	// 免密模式登录接口明确拒绝，而非伪造可绕过的登录态
	rec = httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"token": ""})
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login without token status=%d want 400", rec.Code)
	}
}

func TestFileshareAuthTokenRotationInvalidatesSessions(t *testing.T) {
	server := NewServer(ShareConfig{Port: 0, SharePath: t.TempDir(), AuthToken: "old-pass"}, nil, nil)
	handler := mustHandler(t, server)

	resp := loginFor(t, handler, "old-pass")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d want 200", resp.StatusCode)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	resp.Body.Close()
	if session == nil {
		t.Fatal("no session cookie issued")
	}

	// 运行中改口令：旧 Cookie 立即失效，旧口令 Bearer 同样失效
	server.UpdateConfig(ShareConfig{SharePath: server.configSnapshot().SharePath, AuthToken: "new-pass"})
	req := httptest.NewRequest(http.MethodGet, "/api/list?path=", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale session after rotation status=%d want 401", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/list?path=", nil)
	req.Header.Set("Authorization", "Bearer old-pass")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old token after rotation status=%d want 401", rec.Code)
	}

	resp = loginFor(t, handler, "new-pass")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new token login status=%d want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestFileshareSessionCookieIsTokenDerived(t *testing.T) {
	first := sessionCookieValue("alpha")
	second := sessionCookieValue("beta")
	if first == "" || len(first) != 64 {
		t.Fatalf("unexpected cookie value form: %q", first)
	}
	if first == second {
		t.Fatal("cookie value must derive from token")
	}
	if first == "alpha" || strings.Contains(first, "alpha") {
		t.Fatal("cookie value must not carry plaintext token")
	}
}

// 访客页与 API 全程同源，通配 CORS 反而放行任意网页跨源攻击局域网端点。
func TestFileshareNoWildcardCORS(t *testing.T) {
	server := NewServer(ShareConfig{Port: 0, SharePath: t.TempDir()}, nil, nil)
	handler := mustHandler(t, server)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin=%q want none", got)
	}
}
