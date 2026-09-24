// Package netx 提供"跟随浏览器代理"的 HTTP 客户端公共件（N25 收口）：
// Go 标准库只认 HTTPS_PROXY 环境变量，而主流代理客户端（Clash/v2rayN）默认
// 写的是 WinINET 系统代理（注册表）——浏览器能上 GitHub、Hanxi 却下载失败的
// 落差即源于此（踩坑 #35）。本包按「环境变量优先 → 系统代理兜底 → 直连」
// 链接代理，让全仓 HTTP(S) 出网与用户浏览器走同一条路。
//
// 出身：internal/modules/wsl/netx 孤例（WSL 发行版清单探测专用）经 N25
// 泛型化下沉，wsl 三处调用与托管家族/更新感知/publicip 等全部收编于此；
// 纪律：
//   - 纯 PAC（AutoConfigURL）不解析 JS，如实回落直连——不猜代理地址；
//   - socks= 协议项忽略（net/http 原生 Transport 不接 SOCKS，不做半成品）；
//   - 链上任何一环取不到都是"直连"而非 error——系统代理永不炸穿请求；
//   - http.ProxyFromEnvironment 按进程缓存环境配置（标准库既有行为，#35
//     坑中坑）：链路单测必须走注入式纯函数（chainProxy 四态），不得在
//     进程内改环境变量验证。
//   - 回环/LAN 探测等"必须直连"场景走 DirectClient/LoopbackAwareProxyFunc
//     显式表达（127.0.0.1 被本机 7890 型代理接管即失真），禁止裸 client 碰
//     本机服务。
package netx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Transport 构造带代理链的标准传输层（需要自定义其余字段的调用方用它打底，
// 别从 DefaultTransport 复制——那是"只认 env"的断层源头）。
func Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 ProxyFunc(),
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewClient 构造带代理链的客户端；checkRedirect 传 nil 时用标准库默认策略
// （需要官方主机白名单的调用方自行传入，如 wsl 三处先例）。
func NewClient(timeout time.Duration, checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     Transport(),
		CheckRedirect: checkRedirect,
	}
}

// DirectClient 构造**恒定直连**客户端：回环本机服务、端口/局域网探测、
// 集成测试靶等"过任何代理都失真"的场景专用。超时纪律与 NewClient 同规。
func DirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// ProxyFunc 代理解析链：显式环境变量优先（尊重用户手动配置），
// 未设置时回落到 WinINET 系统代理（浏览器同款出口）。
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	return chainProxy(http.ProxyFromEnvironment, systemProxyURL)
}

// LoopbackAwareProxyFunc 混合闸（artifact.Fetch 形态泛化）：回环恒定直连，
// 其余走代理链。下载内核等"同一 Transport 既打外部 CDN 也可能打本机镜像"
// 的调用方用它；纯外网用 ProxyFunc 即可。
func LoopbackAwareProxyFunc() func(*http.Request) (*url.URL, error) {
	return loopbackAware(ProxyFunc())
}

// loopbackAware 可注入形态（单测锁"回环短路压过任何代理"，不碰真 env）。
func loopbackAware(chain func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if IsLoopbackHost(req.URL.Hostname()) {
			return nil, nil
		}
		return chain(req)
	}
}

// IsLoopbackHost 判定主机名是否回环（localhost / 127.0.0.0/8 / ::1）。
// 自 artifact/fetch.go 上收为公共口径，下载内核反向复用消灭重抄。
func IsLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
