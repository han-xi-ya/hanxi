//go:build windows

package windows

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"hanxi/internal/platform"
)

// ── 扩展表解析与两步取表协议测试（A2 2026-10-26）─────────────────────────

// putU32At 小端写入合成缓冲。
func putU32At(buf []byte, off int, v uint32) { binary.LittleEndian.PutUint32(buf[off:], v) }

// rowV4 合成一行 MIB_TCPROW_OWNER_PID（24 字节定长，dwState 起头）。
func rowV4(state, localAddr, localPort, remoteAddr, remotePort, pid uint32) []byte {
	r := make([]byte, 24)
	putU32At(r, 0, state)
	putU32At(r, 4, localAddr)
	putU32At(r, 8, localPort)
	putU32At(r, 12, remoteAddr)
	putU32At(r, 16, remotePort)
	putU32At(r, 20, pid)
	return r
}

// TestParseMIBTcpTableOwnerPidLayout 对已声明内存布局（127.0.0.1:80 ESTABLISHED
// 形态）做断言：地址按"内存序直读四字节"、端口按网络字节序还原——这是对
// iphlpapi 输出**外部事实**的解码正确性，不是对本实现字符串形状的复读。
func TestParseMIBTcpTableOwnerPidLayout(t *testing.T) {
	buf := make([]byte, 4)
	putU32At(buf, 0, 1)
	// 127.0.0.1 在 dwLocalAddr 中的内存形态 = 字节 7F 00 00 01 → LE uint32 0x0100007F。
	// 80 端口网络序 = 字节 00 50 → 落位 raw uint32 低 16 位 0x5000；9000 = 0x2328，
	// 网络序字节 23 28 → raw 0x2823。
	buf = append(buf, rowV4(5, 0x0100007F, 0x5000, 0x0E02000A, 0x2823, 4321)...)
	rows, err := parseMIBTcpTableOwnerPid(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应解出 1 行，得 %d", len(rows))
	}
	got := rows[0]
	if got.LocalIP != "127.0.0.1" || got.LocalPort != 80 {
		t.Fatalf("本地地址/端口解码失真: %+v", got)
	}
	if got.RemoteIP != "10.0.2.14" || got.RemotePort != 9000 {
		t.Fatalf("远端地址/端口解码失真: %+v", got)
	}
	if got.State != platform.TCPStateEstablished || got.PID != 4321 {
		t.Fatalf("状态/PID 解码失真: %+v", got)
	}
}

// TestParseMIBTablesTruncatedNoPanic 声明 5 行但缓冲只装得下 2 行（探大小
// 与取表之间表收缩的病态窗口）：解析必须截尾自保而非越界读 panic。
func TestParseMIBTablesTruncatedNoPanic(t *testing.T) {
	buf := make([]byte, 4)
	putU32At(buf, 0, 5)
	for i := range 2 {
		buf = append(buf, rowV4(2, 0x0100007F, 0x5000, 0, 0, uint32(i+1))...)
	}
	rows, err := parseMIBTcpTableOwnerPid(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("截断缓冲应解出实际在场 2 行，得 %d", len(rows))
	}

	udpBuf := make([]byte, 4)
	putU32At(udpBuf, 0, 3)
	udpRow := make([]byte, 12) // MIB_UDPROW_OWNER_PID
	putU32At(udpRow, 0, 0x0100007F)
	putU32At(udpRow, 4, 0x3500) // 53（网络序落位）
	putU32At(udpRow, 8, 777)
	udpBuf = append(udpBuf, udpRow...)
	urows, err := parseMIBUdpTableOwnerPid(udpBuf)
	if err != nil {
		t.Fatal(err)
	}
	if len(urows) != 1 || urows[0].LocalPort != 53 || urows[0].LocalIP != "127.0.0.1" || urows[0].PID != 777 {
		t.Fatalf("UDP 行解码失真或行数错误: %+v", urows)
	}

	// 不足头部 4 字节的防御路径：空结果、无错误、无 panic。
	if r, err := parseMIBTcpTableOwnerPid([]byte{}); err != nil || len(r) != 0 {
		t.Fatalf("空缓冲应安全返回空集: %v %v", r, err)
	}
	if r, err := parseMIBUdpTableOwnerPid([]byte{1, 2}); err != nil || len(r) != 0 {
		t.Fatalf("短缓冲应安全返回空集: %v %v", r, err)
	}
}

// TestGetExtendedTableProbeFailureSurfaced 非法表类别（9999）必须如实报错，
// 不得再把"探测失败 + size==0"伪装成"系统无连接的空表"——修复前正是这条
// 吞错路径让上层拿到 0 行且 err==nil，无从区分故障与空闲。
func TestGetExtendedTableProbeFailureSurfaced(t *testing.T) {
	_, err := getExtendedTable(procGetExtendedTcpTable, "GetExtendedTcpTable", 2 /*AF_INET*/, 9999)
	if err == nil {
		t.Fatal("非法 TableClass 探测失败必须上抛错误，不得静默空表")
	}
	t.Logf("探测失败如实上抛（预期）: %v", err)
}

// TestGetExtendedTableHostSmoke 真机取 IPv4 TCP/UDP 全表（普通权限即可）：
// 协议两步走通、返回码 0，行数据形状自洽（端口非零、属主 PID 非零）。
func TestGetExtendedTableHostSmoke(t *testing.T) {
	size := unsafe.Sizeof(mibTcpRowOwnerPid{})
	if size != 24 {
		t.Fatalf("MIB_TCPROW_OWNER_PID 布局漂移: %d", size)
	}
	p := NewPortAPI()
	tcpRows, err := p.TCPTable(platform.FamilyIPv4)
	if err != nil {
		t.Fatalf("真机 TCPTable 失败: %v", err)
	}
	for _, r := range tcpRows[:min(3, len(tcpRows))] {
		if r.PID == 0 {
			t.Fatalf("行属主 PID 恒非零，得 %+v", r)
		}
		if r.State == platform.TCPStateUnknown {
			t.Fatalf("状态码映射表覆盖失真: %+v", r)
		}
	}
	udpRows, err := p.UDPTable(platform.FamilyIPv4)
	if err != nil {
		t.Fatalf("真机 UDPTable 失败: %v", err)
	}
	t.Logf("真机快照：TCP %d 行 / UDP %d 行", len(tcpRows), len(udpRows))
}
