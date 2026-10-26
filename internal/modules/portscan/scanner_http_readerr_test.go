// scanner_http_readerr_test.go 锁 HTTP 指纹探测的"读取中断不得静默"语义。
//
// 旧实现只 `for scanner.Scan()` 之后直接找 <title>，从不查 `scanner.Err()`：
// Scan() 返回 false 既可能是"正常读完"，也可能是"读取出错"——最常见的触发是
// **单行超过 bufio.Scanner 默认 64KiB 上限**（压缩过的无换行 HTML 正是这种形态），
// 另有连接被重置、chunked 截断等。不查 Err 就把"只读到半截页面"静默读成
// "这个站点没有标题"，用户看到的是缺字段的正常结果，无从分辨是截断还是真没有。
//
// 断言对象是**外部可观察的 Banner 文本**，不读 scanner 内部状态。
package portscan

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/netx"
)

// rawHTTPListener 起一个只回原始 HTTP 响应的监听口。
// 不用 httptest.Server：它会自动分块/加换行，无法构造"单个超长无换行行"的形态。
// 每条连接回同样字节后关闭（探测链会先开一条 banner 连接、再开一条 HTTP 连接）。
func rawHTTPListener(t *testing.T, headers, body string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			resp := "HTTP/1.1 200 OK\r\n" + headers + "Connection: close\r\n\r\n" + body
			_, _ = conn.Write([]byte(resp))
			_ = conn.Close()
		}
	}()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("取端口失败: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("端口解析失败: %v", err)
	}
	return port
}

// probeHTTP 走真实探测链（拨号 + HTTP 取标题），返回外部可见结果。
// 回环一律用直连客户端——仓内铁律：访问 127.0.0.1 不得查系统代理。
func probeHTTP(t *testing.T, port int) PortResult {
	t.Helper()
	s := NewScanner()
	res := PortResult{Port: port}
	s.lightweightProbe(context.Background(), &net.Dialer{Timeout: 2 * time.Second},
		netx.DirectClient(3*time.Second), "127.0.0.1", port, 2*time.Second, &res)
	return res
}

// TestHTTPProbeMarksTruncatedRead 单行超 64KiB → Scan 以 ErrTooLong 收束：
// 必须把"未完整读取"标出来，而不是静默给出"没有标题"。
func TestHTTPProbeMarksTruncatedRead(t *testing.T) {
	huge := strings.Repeat("x", 128*1024) // 无换行超长行
	port := rawHTTPListener(t, "Server: BigLineSrv/1.0\r\n", "<html><head><title>"+huge)
	res := probeHTTP(t, port)

	if !strings.Contains(res.Banner, "响应未完整读取") {
		t.Fatalf("超长无换行响应被截断读取，Banner 却未标注中断，实际 %q", res.Banner)
	}
	if strings.Contains(res.Banner, "Title:") {
		t.Fatalf("页面未完整读取却仍报出了标题（会把截断处的伪标题当真）: %q", res.Banner)
	}
	// Server 头是在响应头里读到的，与 body 截断无关，应照常保留。
	if !strings.Contains(res.Banner, "BigLineSrv") {
		t.Errorf("响应头信息被一并丢弃，实际 %q", res.Banner)
	}
}

// TestHTTPProbeNormalReadNotMarked 反向护栏：正常小页面必须**不**带中断标注，
// 标题照常提取——防止为了"如实"而把所有响应都标成可疑。
func TestHTTPProbeNormalReadNotMarked(t *testing.T) {
	port := rawHTTPListener(t, "Server: NormalSrv/2.0\r\n",
		"<html><head><title>正常站点</title></head><body>ok</body></html>")
	res := probeHTTP(t, port)

	if strings.Contains(res.Banner, "响应未完整读取") {
		t.Fatalf("完整读取的正常响应被误标为中断: %q", res.Banner)
	}
	if !strings.Contains(res.Banner, "Title: 正常站点") {
		t.Fatalf("正常标题未被提取: %q", res.Banner)
	}
}
