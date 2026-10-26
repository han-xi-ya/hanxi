package version

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestProbeClientReusesConnection 断言外部可观察事实：probeClient 经 fetchPage
// 先后两发 GET 打到同一回环靶，服务端只观察到一条新建 TCP 连接（keep-alive
// 复用）。修复前 probeAssets 与 fetchPage 各自逐请求新建 Transport——N 个
// release 的一轮探测会另起 2N+ 套连接池，靶实测会观察到多条新建连接。
func TestProbeClientReusesConnection(t *testing.T) {
	var newConns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "abc123  Everything-1.0.x64.zip\n")
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	srv.Start()
	defer srv.Close()

	for i := 1; i <= 2; i++ {
		body, err := fetchPage(srv.URL, 1<<20)
		if err != nil {
			t.Fatalf("第 %d 次 fetchPage 失败: %v", i, err)
		}
		if len(body) == 0 {
			t.Fatalf("第 %d 次 fetchPage 空响应体", i)
		}
	}
	if got := atomic.LoadInt32(&newConns); got != 1 {
		t.Fatalf("两发请求应复用一条连接：靶实测新建 %d 条——Transport 被逐调用重建？", got)
	}
}
