package instance

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestIsOCRServiceDirectDespiteEnvProxy "回环 HTTP 禁代理"铁律的真实验收面：
// 把 HTTP_PROXY/http_proxy 指向一只记录型代理口（任何经代理的连接都会被
// Accept 计数并掐断），契约探测必须照常判"是 hanxi-ocr"且代理口零连接。
// 断言的是外部可观察事实（连了谁、探测结论对不对），不是 Transport 字段形状
// ——将来谁把直连件漂回"只认 env"，本测试当场红。
func TestIsOCRServiceDirectDespiteEnvProxy(t *testing.T) {
	var proxyHits atomic.Int32
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyLn.Close()
	go func() {
		for {
			c, aerr := proxyLn.Accept()
			if aerr != nil {
				return
			}
			proxyHits.Add(1)
			_ = c.Close()
		}
	}()

	t.Setenv("HTTP_PROXY", "http://"+proxyLn.Addr().String())
	t.Setenv("http_proxy", "http://"+proxyLn.Addr().String())
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"name":"hanxi-ocr"}`))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	var p netPortProbe
	if !p.IsOCRService(addr) {
		t.Fatalf("回环契约探测在 env 代理在场时必须照常命中（%s）", addr)
	}
	if n := proxyHits.Load(); n != 0 {
		t.Fatalf("回环请求被路由去了代理（代理口连接 %d 次）", n)
	}
}
