package version

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestHeadClientReusesConnection 断言外部可观察事实：同一 headClient() 先后两发
// HEAD 打到同一回环靶，服务端只观察到一条新建 TCP 连接（keep-alive 复用）。
// 修复前每次调用新建 Transport（各自独立连接池）——靶会观察到两条新建连接。
func TestHeadClientReusesConnection(t *testing.T) {
	var newConns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "42")
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	srv.Start()
	defer srv.Close()

	for i := 1; i <= 2; i++ {
		resp, err := headClient().Head(srv.URL)
		if err != nil {
			t.Fatalf("第 %d 次 HEAD 失败: %v", i, err)
		}
		resp.Body.Close()
		if resp.ContentLength != 42 {
			t.Fatalf("第 %d 次 HEAD 响应失真: %+v", i, resp)
		}
	}
	if got := atomic.LoadInt32(&newConns); got != 1 {
		t.Fatalf("两发 HEAD 应复用一条连接：靶实测新建 %d 条——Transport 被逐调用重建？", got)
	}
}
