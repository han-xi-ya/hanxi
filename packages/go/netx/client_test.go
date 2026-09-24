package netx

import (
	"net/http"
	"net/url"
	"testing"
)

func TestParseProxyServer(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"127.0.0.1:7897", "http://127.0.0.1:7897"},        // Clash/v2rayN 全局直连型（用户实机形态）
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890"}, // 已带 scheme 原样通过
		{"http=a:1;https=b:2", "http://b:2"},               // 协议分流取 https
		{"http=a:1;socks=s:9", "http://a:1"},               // 无 https 回落 http
		{"https=b:2;ftp=x", "http://b:2"},                  // 顺序无关
		{"unknown=zzz", ""},                                // 无可用协议项
	}
	for _, c := range cases {
		if got := parseProxyServer(c.in); got != c.want {
			t.Fatalf("parseProxyServer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestChainProxy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	sysURL := "http://127.0.0.1:7897"

	// 1) 环境变量命中：必须压过系统代理。
	envHit := func(*http.Request) (*url.URL, error) { return url.Parse("http://env:1") }
	u, err := chainProxy(envHit, func() string { return sysURL })(req)
	if err != nil || u == nil || u.Host != "env:1" {
		t.Fatalf("env 优先级失守: %v %v", u, err)
	}

	// 2) 环境变量未设置：回落系统代理。
	envMiss := func(*http.Request) (*url.URL, error) { return nil, nil }
	u, err = chainProxy(envMiss, func() string { return sysURL })(req)
	if err != nil || u == nil || u.String() != sysURL {
		t.Fatalf("系统代理兜底失败: %v %v", u, err)
	}

	// 3) 两路都没有：直连（nil）。
	u, err = chainProxy(envMiss, func() string { return "" })(req)
	if err != nil || u != nil {
		t.Fatalf("应直连: %v %v", u, err)
	}

	// 4) 系统代理值畸形：安全回落直连而非报错炸穿请求。
	u, err = chainProxy(envMiss, func() string { return "::://::坏值" })(req)
	if err != nil || u != nil {
		t.Fatalf("畸形系统代理应直连兜底: %v %v", u, err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"localhost", "LOCALHOST", "127.0.0.1", "127.1.2.3", "::1", "[::1]"} {
		if !IsLoopbackHost(h) {
			t.Errorf("%q 应判回环", h)
		}
	}
	for _, h := range []string{"", "api.github.com", "10.0.0.8", "192.168.1.31", "::2"} {
		if IsLoopbackHost(h) {
			t.Errorf("%q 不应判回环", h)
		}
	}
}

// 回环绕代理短路：链上代理再怎么命中，回环目标必须恒直连
// （本机 7890 型代理接管 127.0.0.1 测试靶/本机服务即失真——#35 镜像纪律）。
func TestLoopbackAwareShortCircuits(t *testing.T) {
	alwaysProxy := func(*http.Request) (*url.URL, error) { return url.Parse("http://127.0.0.1:7890") }
	chain := loopbackAware(alwaysProxy)
	for _, target := range []string{"http://127.0.0.1:8080/x", "http://localhost:9/x", "https://[::1]:5/y"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if u, err := chain(req); err != nil || u != nil {
			t.Fatalf("回环必须直连（%s）: %v %v", target, u, err)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	if u, err := chain(req); err != nil || u == nil {
		t.Fatalf("外部目标应走链: %v %v", u, err)
	}
}
