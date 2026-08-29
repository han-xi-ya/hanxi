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
)

type PortImpl struct{}

func NewPortAPI() platform.PortAPI {
	return &PortImpl{}
}

// TCPTable 获取系统 TCP 表（目前支持 IPv4，扩展 IPv6）
func (p *PortImpl) TCPTable(family platform.Family) ([]platform.TCPRow, error) {
	if family != platform.FamilyIPv4 && family != platform.FamilyIPv6 {
		return nil, platform.ErrNotSupported
	}

	af := uint32(syscall.AF_INET)
	if family == platform.FamilyIPv6 {
		af = uint32(syscall.AF_INET6)
	}

	var size uint32
	// 首次调用获取所需缓冲区大小
	ret, _, _ := procGetExtendedTcpTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		1, // sorted
		uintptr(af),
		uintptr(tcpTableOwnerPidAll),
		0,
	)

	if size == 0 {
		return []platform.TCPRow{}, nil
	}

	buf := make([]byte, size)
	ret, _, err := procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		1,
		uintptr(af),
		uintptr(tcpTableOwnerPidAll),
		0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable failed with code %d: %w", ret, err)
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

	af := uint32(syscall.AF_INET)
	if family == platform.FamilyIPv6 {
		af = uint32(syscall.AF_INET6)
	}

	var size uint32
	ret, _, _ := procGetExtendedUdpTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		1,
		uintptr(af),
		uintptr(udpTableOwnerPid),
		0,
	)

	if size == 0 {
		return []platform.UDPRow{}, nil
	}

	buf := make([]byte, size)
	ret, _, err := procGetExtendedUdpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		1,
		uintptr(af),
		uintptr(udpTableOwnerPid),
		0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedUdpTable failed with code %d: %w", ret, err)
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
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.TCPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPid{})

	offset := 4
	for i := uint32(0); i < numEntries; i++ {
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
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.UDPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibUdpRowOwnerPid{})

	offset := 4
	for i := uint32(0); i < numEntries; i++ {
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
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.TCPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibTcp6RowOwnerPid{})

	offset := 4
	for i := uint32(0); i < numEntries; i++ {
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
	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	rows := make([]platform.UDPRow, 0, numEntries)
	rowSize := unsafe.Sizeof(mibUdp6RowOwnerPid{})

	offset := 4
	for i := uint32(0); i < numEntries; i++ {
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
