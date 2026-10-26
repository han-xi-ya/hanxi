package netx

// A1 审查批次：DirectClient 恒定直连承诺与"传输层同源打底"的防漂移锁。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDirectClientNeverProxies(t *testing.T) {
	c := DirectClient(5 * time.Second)
	if c.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %v, want 5s", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型异常: %T", c.Transport)
	}
	// 结构性承诺：Proxy 钩子为 nil 时标准库根本不会去查 env/系统代理——
	// 任何"本机服务探测"都不可能被 7890 型代理接管失真。
	if tr.Proxy != nil {
		t.Fatal("直连客户端的 Proxy 钩子必须为 nil")
	}
	if tr.DialContext == nil {
		t.Fatal("直连客户端不得丢失拨号参数（连接超时/KeepAlive）")
	}
	if tr.TLSHandshakeTimeout <= 0 {
		t.Fatal("直连客户端必须保留 TLS 握手上限")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("直连请求回环测试靶失败: %v", err)
	}
	defer resp.Body.Close()
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil || string(body) != "pong" {
		t.Fatalf("端到端直连读回 %q (err %v), want pong", body, rerr)
	}
}

// TestDirectClientSharesTransportDiscipline 锁"DirectClient 与 NewClient 的
// 传输纪律同源、只差代理钩子"：打底改动后若有人只调一处，本测试即红。
func TestDirectClientSharesTransportDiscipline(t *testing.T) {
	d := DirectClient(time.Second).Transport.(*http.Transport)
	n := NewClient(time.Second, nil).Transport.(*http.Transport)
	if d.Proxy != nil {
		t.Fatal("直连件不得挂代理钩子")
	}
	if n.Proxy == nil {
		t.Fatal("代理链件的代理钩子不得被顺带摘除")
	}
	if d.TLSHandshakeTimeout != n.TLSHandshakeTimeout ||
		d.IdleConnTimeout != n.IdleConnTimeout ||
		d.MaxIdleConns != n.MaxIdleConns ||
		d.ForceAttemptHTTP2 != n.ForceAttemptHTTP2 ||
		d.ExpectContinueTimeout != n.ExpectContinueTimeout {
		t.Fatal("直连件传输纪律已与代理链件漂移（两处必须同源打底）")
	}
}
