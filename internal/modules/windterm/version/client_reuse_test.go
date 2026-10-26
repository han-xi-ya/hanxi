package version

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestAPIClientReusesConnection 断言外部可观察事实：同一 apiClient() 先后两发
// 请求打到同一回环靶，服务端只观察到一条新建 TCP 连接（keep-alive 复用）。
// 修复前每次调用新建 Transport（各自独立连接池）——靶会观察到两条新建连接。
// 本测试锁"连接复用"这一外证，不锁"指针相同"之类的实现细节。
func TestAPIClientReusesConnection(t *testing.T) {
	var newConns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	srv.Start()
	defer srv.Close()

	for i := 1; i <= 2; i++ {
		resp, err := apiClient().Get(srv.URL)
		if err != nil {
			t.Fatalf("第 %d 次请求失败: %v", i, err)
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil || len(body) == 0 {
			t.Fatalf("第 %d 次响应体读取失真: %v", i, rerr)
		}
	}
	if got := atomic.LoadInt32(&newConns); got != 1 {
		t.Fatalf("两发请求应复用一条连接：靶实测新建 %d 条——Transport 被逐调用重建？", got)
	}
}
