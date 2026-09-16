package fileshare

// HTTP 响应与展示层小工具：显式长度 JSON 写出、客户端 IP 归一、体积人性化、
// 以及统计用响应计数字节器。与传输引擎本身无耦合，单独成文件便于取用。

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// writeJSON 显式 Content-Length 写出 JSON 响应
// (避免隐式 chunked 流式响应在部分 WebView/移动浏览器环境挂起)
func writeJSON(w http.ResponseWriter, status int, data any) {
	body, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// countingResponseWriter 包装 ResponseWriter 以统计实际写入客户端的字节数
type countingResponseWriter struct {
	http.ResponseWriter
	n int64
}

// Write 透传响应写入并累计字节数；无 Flush/Hijack 等可选接口需求，故不转发。
func (w *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
