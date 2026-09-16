package instance

import (
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// loopProbeProbe 跨平台 TCP 拨测与 OCR 服务契约探测（各平台探测结构体内嵌复用）。
// 400ms 连接超时：回环地址正常握手在毫秒级，慢仅出现在半开/防火墙丢弃场景。
type netPortProbe struct{}

func (netPortProbe) PortOpen(listenAddr string) bool {
	conn, err := net.DialTimeout("tcp", listenAddr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ocrStatusClient 契约探测专用短超时客户端。
// Proxy 必须显式置 nil：回环请求一旦被系统代理（如 127.0.0.1:7890）接管，
// 探测与转发会整体失真且极难排查（hanxi 已在 wsl/netx 场景付过学费）。
var ocrStatusClient = &http.Client{
	Timeout:   800 * time.Millisecond,
	Transport: &http.Transport{Proxy: nil},
}

// IsOCRService GET http://<addr>/api/status，断言 ok 且 name 为 hanxi-ocr。
// 容忍未知字段（上游演进只加不减）。
func (netPortProbe) IsOCRService(listenAddr string) bool {
	resp, err := ocrStatusClient.Get("http://" + listenAddr + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var v struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return false
	}
	return v.OK && v.Name == "hanxi-ocr"
}
