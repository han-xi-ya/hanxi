package instance

import (
	"net"
	"time"
)

// netPortProbe 跨平台 TCP 拨测实现（各平台探测结构体内嵌复用）。
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
