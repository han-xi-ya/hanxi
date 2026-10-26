//go:build windows

package windows

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"hanxi/internal/platform"
)

var (
	modIphlpapi = syscall.NewLazyDLL("iphlpapi.dll")

	procGetExtendedTcpTable = modIphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUdpTable = modIphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	tcpTableOwnerPidAll = 5
	udpTableOwnerPid    = 1

	// errInsufficientBuffer ERROR_INSUFFICIENT_BUFFER（winerror.h 122）：
	// 二次取表期间连接表膨胀时的唯一回码，API 同步把所需大小写回 size。
	errInsufficientBuffer = 122
	// extendedTableMaxFetch 二次取表重试上限：高压 churn 下表可能连续膨胀，
	// 封顶防活锁（到顶如实报错，不静默降级为空表）。
	extendedTableMaxFetch = 4
)

// PortImpl platform.PortAPI 的 Windows 实现：经 iphlpapi 扩展表 API 获取带属主 PID 的连接表。
type PortImpl struct{}

// NewPortAPI 返回端口与连接表 API 实例。
func NewPortAPI() platform.PortAPI {
	return &PortImpl{}
}

// getExtendedTable 执行 iphlpapi 扩展表"探大小→取全表"两步协议（TCP/UDP、
// IPv4/IPv6 四路共用，收口此前两份逐字克隆）。
//
// 审查修复（A2 2026-10-26）两处：
//   - 探测调用失败曾被吞没——size==0 且探测返回码非 0 时旧实现报"空表 + 无
//     错误"，非法参数/接口故障被伪装成"系统无连接"，调用方无从分辨；现在
//     如实上抛错误码。
//   - 探测与取表之间表若膨胀，二次调用回 ERROR_INSUFFICIENT_BUFFER 且 API
//     已把新所需大小写回 size——旧实现直接报错，高频连接抖动下端口扫描偶发
//     假失败；现在按回报大小有限重试。
func getExtendedTable(proc *syscall.LazyProc, name string, af, tableClass uintptr) ([]byte, error) {
	var size uint32
	probe, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&size)), 1, af, tableClass, 0)
	if size == 0 {
		if probe == 0 {
			return []byte{}, nil // 成功且回报 0 字节：视为确无数据（理论形态，防御保留）
		}
		return nil, fmt.Errorf("%s probe failed with code %d", name, probe)
	}
	for range extendedTableMaxFetch {
		buf := make([]byte, size)
		ret, _, err := proc.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			1, af, tableClass, 0,
		)
		if ret == 0 {
			return buf, nil
		}
		if ret != errInsufficientBuffer || size == 0 {
			return nil, fmt.Errorf("%s failed with code %d: %w", name, ret, err)
		}
	}
	return nil, fmt.Errorf("%s failed after %d retries: connection table keeps growing", name, extendedTableMaxFetch)
}

// TCPTable 获取系统 TCP 表：IPv4/IPv6 均由 GetExtendedTcpTable 一次调用快照；
// bOrder=1 系统按本机地址+本机端口升序回报（netstat 同序实证；并非按远端地址）。
func (p *PortImpl) TCPTable(family platform.Family) ([]platform.TCPRow, error) {
	if family != platform.FamilyIPv4 && family != platform.FamilyIPv6 {
		return nil, platform.ErrNotSupported
	}

	af := uintptr(syscall.AF_INET)
	if family == platform.FamilyIPv6 {
		af = syscall.AF_INET6
	}

	buf, err := getExtendedTable(procGetExtendedTcpTable, "GetExtendedTcpTable", af, tcpTableOwnerPidAll)
	if err != nil {
		return nil, err
	}

	if family == platform.FamilyIPv4 {
		return parseMIBTcpTableOwnerPid(buf)
	}
	return parseMIBTcp6TableOwnerPid(buf)
}

// UDPTable 获取系统 UDP 表
func (p *PortImpl) UDPTable(family platform.Family) ([]platform.UDPRow, error) {
	if family != platform.FamilyIPv4 && family != platform.FamilyIPv6 {
		return nil, platform.ErrNotSupported
	}

	af := uintptr(syscall.AF_INET)
	if family == platform.FamilyIPv6 {
		af = syscall.AF_INET6
	}

	buf, err := getExtendedTable(procGetExtendedUdpTable, "GetExtendedUdpTable", af, udpTableOwnerPid)
	if err != nil {
		return nil, err
	}

	if family == platform.FamilyIPv4 {
		return parseMIBUdpTableOwnerPid(buf)
	}
	return parseMIBUdp6TableOwnerPid(buf)
}

// 结构对齐与内存解析
type mibTcpRowOwnerPid struct {
	dwState      uint32
	dwLocalAddr  uint32
	dwLocalPort  uint32
	dwRemoteAddr uint32
	dwRemotePort uint32
	dwOwningPid  uint32
}

func parseMIBTcpTableOwnerPid(buf []byte) ([]platform.TCPRow, error) {
	if len(buf) < 4 {
		return nil, nil // 防御：空缓冲无头部可读（getExtendedTable 防御路径）
	}
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.TCPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPid{})

	offset := 4
	for range numEntries {
		if offset+int(rowSize) > len(buf) {
			break
		}
		rawRow := (*mibTcpRowOwnerPid)(unsafe.Pointer(&buf[offset]))
		offset += int(rowSize)

		localIP := net.IPv4(byte(rawRow.dwLocalAddr), byte(rawRow.dwLocalAddr>>8), byte(rawRow.dwLocalAddr>>16), byte(rawRow.dwLocalAddr>>24)).String()
		remoteIP := net.IPv4(byte(rawRow.dwRemoteAddr), byte(rawRow.dwRemoteAddr>>8), byte(rawRow.dwRemoteAddr>>16), byte(rawRow.dwRemoteAddr>>24)).String()

		localPort := decodePort(rawRow.dwLocalPort)
		remotePort := decodePort(rawRow.dwRemotePort)

		rows = append(rows, platform.TCPRow{
			LocalIP:    localIP,
			LocalPort:  localPort,
			RemoteIP:   remoteIP,
			RemotePort: remotePort,
			State:      mapTcpState(rawRow.dwState),
			PID:        rawRow.dwOwningPid,
		})
	}
	return rows, nil
}

type mibUdpRowOwnerPid struct {
	dwLocalAddr uint32
	dwLocalPort uint32
	dwOwningPid uint32
}

func parseMIBUdpTableOwnerPid(buf []byte) ([]platform.UDPRow, error) {
	if len(buf) < 4 {
		return nil, nil // 防御：空缓冲无头部可读（getExtendedTable 防御路径）
	}
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.UDPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibUdpRowOwnerPid{})

	offset := 4
	for range numEntries {
		if offset+int(rowSize) > len(buf) {
			break
		}
		rawRow := (*mibUdpRowOwnerPid)(unsafe.Pointer(&buf[offset]))
		offset += int(rowSize)

		localIP := net.IPv4(byte(rawRow.dwLocalAddr), byte(rawRow.dwLocalAddr>>8), byte(rawRow.dwLocalAddr>>16), byte(rawRow.dwLocalAddr>>24)).String()
		localPort := decodePort(rawRow.dwLocalPort)

		rows = append(rows, platform.UDPRow{
			LocalIP:   localIP,
			LocalPort: localPort,
			PID:       rawRow.dwOwningPid,
		})
	}
	return rows, nil
}

// IPv6 解析
type mibTcp6RowOwnerPid struct {
	ucLocalAddr     [16]byte
	dwLocalScopeId  uint32
	dwLocalPort     uint32
	ucRemoteAddr    [16]byte
	dwRemoteScopeId uint32
	dwRemotePort    uint32
	dwState         uint32
	dwOwningPid     uint32
}

func parseMIBTcp6TableOwnerPid(buf []byte) ([]platform.TCPRow, error) {
	if len(buf) < 4 {
		return nil, nil // 防御：空缓冲无头部可读（getExtendedTable 防御路径）
	}
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.TCPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibTcp6RowOwnerPid{})

	offset := 4
	for range numEntries {
		if offset+int(rowSize) > len(buf) {
			break
		}
		rawRow := (*mibTcp6RowOwnerPid)(unsafe.Pointer(&buf[offset]))
		offset += int(rowSize)

		localIP := net.IP(rawRow.ucLocalAddr[:]).String()
		remoteIP := net.IP(rawRow.ucRemoteAddr[:]).String()
		localPort := decodePort(rawRow.dwLocalPort)
		remotePort := decodePort(rawRow.dwRemotePort)

		rows = append(rows, platform.TCPRow{
			LocalIP:    localIP,
			LocalPort:  localPort,
			RemoteIP:   remoteIP,
			RemotePort: remotePort,
			State:      mapTcpState(rawRow.dwState),
			PID:        rawRow.dwOwningPid,
		})
	}
	return rows, nil
}

type mibUdp6RowOwnerPid struct {
	ucLocalAddr    [16]byte
	dwLocalScopeId uint32
	dwLocalPort    uint32
	dwOwningPid    uint32
}

func parseMIBUdp6TableOwnerPid(buf []byte) ([]platform.UDPRow, error) {
	if len(buf) < 4 {
		return nil, nil // 防御：空缓冲无头部可读（getExtendedTable 防御路径）
	}
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.UDPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibUdp6RowOwnerPid{})

	offset := 4
	for range numEntries {
		if offset+int(rowSize) > len(buf) {
			break
		}
		rawRow := (*mibUdp6RowOwnerPid)(unsafe.Pointer(&buf[offset]))
		offset += int(rowSize)

		localIP := net.IP(rawRow.ucLocalAddr[:]).String()
		localPort := decodePort(rawRow.dwLocalPort)

		rows = append(rows, platform.UDPRow{
			LocalIP:   localIP,
			LocalPort: localPort,
			PID:       rawRow.dwOwningPid,
		})
	}
	return rows, nil
}

// 端口网络字节序转主机字节序 (Big-Endian to Host uint16)
func decodePort(rawPort uint32) uint16 {
	return binary.BigEndian.Uint16([]byte{byte(rawPort), byte(rawPort >> 8)})
}

func mapTcpState(state uint32) platform.TCPState {
	switch state {
	case 1:
		return platform.TCPStateClosed
	case 2:
		return platform.TCPStateListen
	case 3:
		return platform.TCPStateSynSent
	case 4:
		return platform.TCPStateSynReceived
	case 5:
		return platform.TCPStateEstablished
	case 6:
		return platform.TCPStateFinWait1
	case 7:
		return platform.TCPStateFinWait2
	case 8:
		return platform.TCPStateCloseWait
	case 9:
		return platform.TCPStateClosing
	case 10:
		return platform.TCPStateLastAck
	case 11:
		return platform.TCPStateTimeWait
	case 12:
		return platform.TCPStateDeleteTCB
	default:
		return platform.TCPStateUnknown
	}
}
