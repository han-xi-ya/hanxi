package fileshare

// 口令门禁与登录态：会话 Cookie 由访问口令 HMAC 派生（无服务端会话表），
// 免密模式（AuthToken 为空）保持「局域网免密共享」的产品语义直接放行。

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// 口令会话 Cookie：HttpOnly + SameSite=Lax，登录一次有效期内免再输；
// 跨站 POST 不带 Cookie，天然免疫 CSRF 借权。
const (
	sessionCookieName   = "hanxi_share_session"
	sessionCookieMaxAge = 30 * 24 * time.Hour
)

// loginPenaltyDelay 口令校验失败后的统一延时：压缩局域网内口令爆破的尝试频率
// （写死在 handler 里曾让人误以为可调，收口成具名常量）。
const loginPenaltyDelay = 400 * time.Millisecond

// sessionCookieValue 由访问口令 HMAC 派生会话 Cookie 值：口令不落 Cookie，
// 换口令即令全部旧 Cookie 失效（无需服务端会话表，重启后旧登录态仍有效）。
func sessionCookieValue(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("hanxi-fileshare/session-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// handleLogin 校验访问口令并签发会话 Cookie。
// 免密模式下明确拒绝（400），避免调用方误以为存在可绕过的登录态。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.configSnapshot()
	if cfg.AuthToken == "" {
		http.Error(w, "本共享未设置访问口令", http.StatusBadRequest)
		return
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload); err != nil {
		http.Error(w, "请求体不合法", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(payload.Token), []byte(cfg.AuthToken)) != 1 {
		// 失败路径统一延时，压缩局域网内口令爆破的尝试频率
		time.Sleep(loginPenaltyDelay)
		http.Error(w, "访问口令不正确", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionCookieValue(cfg.AuthToken),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionCookieMaxAge / time.Second),
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// authGate 口令门禁：AuthToken 为空保持免密语义（产品定位「免密局域网共享」）；
// 非空时页面与静态资源放行（登录界面自身需要加载），/api/login 与 /api/config
// 白名单放行，其余 /api/*（列表/下载/预览/上传/投递/统计）必须持有有效会话。
func (s *Server) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.configSnapshot().AuthToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case !strings.HasPrefix(r.URL.Path, "/api/"),
			r.URL.Path == "/api/login",
			r.URL.Path == "/api/config":
			next.ServeHTTP(w, r)
		case s.authenticated(r):
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "需要访问口令", http.StatusUnauthorized)
		}
	})
}

// authenticated 双通道校验：浏览器走会话 Cookie（HMAC 派生值，恒时比较），
// 非浏览器集成（脚本/快捷指令等）支持 Authorization: Bearer <口令>。
func (s *Server) authenticated(r *http.Request) bool {
	cfg := s.configSnapshot()
	want := sessionCookieValue(cfg.AuthToken)
	if cookie, err := r.Cookie(sessionCookieName); err == nil &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) == 1 {
		return true
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok &&
		subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AuthToken)) == 1 {
		return true
	}
	return false
}
