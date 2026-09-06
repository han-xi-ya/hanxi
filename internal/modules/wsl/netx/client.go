// Package netx 提供"跟随浏览器代理"的 HTTP 客户端：
// Go 标准库只认 HTTPS_PROXY 环境变量，而主流代理客户端默认写的是
// WinINET 系统代理（注册表）——浏览器能上 GitHub、Hanxi 却 403 的落差即源于此。
// 本包按「环境变量优先 → 系统代理兜底」链接代理，让直连被拦截时
// WSL 模块的 GitHub 请求与用户浏览器走同一条路。
package netx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewClient 构造带代理链与官方主机重定向守卫的客户端。
// checkRedirect 传 nil 时仅保留 https 协议约束。
func NewClient(timeout time.Duration, checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 ProxyFunc(),
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: checkRedirect,
	}
}

// ProxyFunc 代理解析链：显式环境变量优先（尊重用户手动配置），
// 未设置时回落到 WinINET 系统代理（浏览器同款出口）。
// 注：http.ProxyFromEnvironment 按进程缓存环境配置，属标准库既有行为。
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	return chainProxy(http.ProxyFromEnvironment, systemProxyURL)
}

// chainProxy 拆成可注入形态便于单测（env → 系统代理 → 直连）。
func chainProxy(env func(*http.Request) (*url.URL, error), sys func() string) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if u, err := env(req); err != nil || u != nil {
			return u, err
		}
		if proxy := sys(); proxy != "" {
			if u, err := url.Parse(proxy); err == nil {
				return u, nil
			}
		}
		return nil, nil
	}
}

// parseProxyServer 解析 WinINET ProxyServer 值，形态：
//   - "127.0.0.1:7890"                    → 全局一个代理
//   - "http=h1:8080;https=h2:8443;socks=s" → 按协议分流（HTTPS 请求优先 https=，回落 http=）
//
// 返回带 scheme 的代理 URL；无法解析返回空串。
func parseProxyServer(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "=") {
		return withScheme(raw)
	}
	fallback := ""
	for _, part := range strings.Split(raw, ";") {
		scheme, server, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || server == "" {
			continue
		}
		switch strings.ToLower(scheme) {
		case "https":
			return withScheme(server)
		case "http":
			if fallback == "" {
				fallback = withScheme(server)
			}
		}
	}
	return fallback
}

func withScheme(server string) string {
	if strings.Contains(server, "://") {
		return server
	}
	return "http://" + server // CONNECT 隧道：https 流量也经 http 代理转发
}
