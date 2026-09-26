package instance

import (
	"fmt"
	"net"
	"time"
)

// exeImageName piik 主进程名（托管版本目录内唯一入口 exe）。runtime 双
// exe 孙进程为异名（piik-capture.exe/cloudflared.exe），不污染进程名计数，
// 也绝不能作为存在性判据（它们是可选载荷：仅 LAN 邀请/公网隧道开启时在场）。
const exeImageName = "piik-app.exe"

// portDialTimeout 回环 TCP 拨测超时：正常握手毫秒级，慢仅出现在半开/
// 防火墙丢弃场景（ddnsgo/ocr 同值 400ms）。
var portDialTimeout = 400 * time.Millisecond

// netPortProbe 跨平台端口 LISTEN 拨测实现（各平台探测结构体内嵌复用）。
// TCP 握手即算 LISTEN（不发送应用数据），连接失败/超时统一视为未就绪。
type netPortProbe struct{}

func (netPortProbe) PortListening(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, portDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
