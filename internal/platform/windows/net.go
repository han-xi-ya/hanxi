//go:build windows

package windows

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"hubkit/internal/platform"
)

var (
	procIcmpCreateFile  = modIphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = modIphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = modIphlpapi.NewProc("IcmpSendEcho")

	procGetIpNetTable2 = modIphlpapi.NewProc("GetIpNetTable2")
	procFreeMibTable   = modIphlpapi.NewProc("FreeMibTable")
)

type NetworkImpl struct{}

func NewNetworkAPI() platform.NetworkAPI {
	return &NetworkImpl{}
}

// Adapters 列举本机可用网卡与地址
func (n *NetworkImpl) Adapters() ([]platform.Adapter, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("net.Interfaces failed: %w", err)
	}

	result := make([]platform.Adapter, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		var ipv4List, ipv6List []string
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				ipv4List = append(ipv4List, ip4.String())
			} else if ip6 := ipNet.IP.To16(); ip6 != nil {
				ipv6List = append(ipv6List, ip6.String())
			}
		}

		isLoopback := (iface.Flags & net.FlagLoopback) != 0
		isUp := (iface.Flags & net.FlagUp) != 0
		isPhysical := !isLoopback && len(iface.HardwareAddr) > 0

		result = append(result, platform.Adapter{
			Index:       uint32(iface.Index),
			Name:        iface.Name,
			Description: iface.Name,
			MAC:         iface.HardwareAddr.String(),
			IPv4:        ipv4List,
			IPv6:        ipv6List,
			IsPhysical:  isPhysical,
			IsLoopback:  isLoopback,
			IsUp:        isUp,
		})
	}
	return result, nil
}

// DefaultAdapter 获取带有默认路由/主 IP 的网卡
func (n *NetworkImpl) DefaultAdapter() (*platform.Adapter, error) {
	adapters, err := n.Adapters()
	if err != nil {
		return nil, err
	}
	for _, a := range adapters {
		if a.IsUp && !a.IsLoopback && len(a.IPv4) > 0 {
			return &a, nil
		}
	}
	if len(adapters) > 0 {
		return &adapters[0], nil
	}
	return nil, fmt.Errorf("no active network adapter found")
}

type mibIpNetRow2 struct {
	Address            [28]byte // SOCKADDR_INET
	InterfaceIndex     uint32
	InterfaceLuid      uint64
	PhysicalAddress    [32]byte
	PhysicalAddressLen uint32
	State              uint32
	Flags              uint32
	ReachabilityTime   uint32
}

type mibIpNetTable2 struct {
	NumEntries uint32
	Table      [1]mibIpNetRow2
}

// NeighborTable 读取系统 ARP / 邻居缓存表
func (n *NetworkImpl) NeighborTable() ([]platform.Neighbor, error) {
	var pTable *mibIpNetTable2
	r, _, err := procGetIpNetTable2.Call(
		uintptr(syscall.AF_UNSPEC),
		uintptr(unsafe.Pointer(&pTable)),
	)
	if r != 0 || pTable == nil {
		return nil, fmt.Errorf("GetIpNetTable2 failed: %w", err)
	}
	defer procFreeMibTable.Call(uintptr(unsafe.Pointer(pTable)))

	numEntries := pTable.NumEntries
	if numEntries == 0 {
		return []platform.Neighbor{}, nil
	}

	// 按照 slice 结构映射多行连续内存
	rows := unsafe.Slice(&pTable.Table[0], numEntries)
	result := make([]platform.Neighbor, 0, numEntries)

	for _, row := range rows {
		// 解析 IP (IPv4: family == AF_INET == 2)
		family := binary.LittleEndian.Uint16(row.Address[0:2])
		var ipStr string
		if family == uint16(syscall.AF_INET) {
			ip := net.IPv4(row.Address[4], row.Address[5], row.Address[6], row.Address[7])
			ipStr = ip.String()
		} else if family == uint16(syscall.AF_INET6) {
			ipStr = net.IP(row.Address[8:24]).String()
		}

		if ipStr == "" {
			continue
		}

		// 解析 MAC
		var macStr string
		if row.PhysicalAddressLen > 0 && row.PhysicalAddressLen <= 32 {
			macBytes := row.PhysicalAddress[:row.PhysicalAddressLen]
			macStr = net.HardwareAddr(macBytes).String()
		}

		result = append(result, platform.Neighbor{
			IP:        ipStr,
			MAC:       macStr,
			Interface: row.InterfaceIndex,
			State:     fmt.Sprintf("%d", row.State),
		})
	}

	return result, nil
}

type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       struct {
		Ttl         byte
		Tos         byte
		Flags       byte
		OptionsSize byte
		OptionsData uintptr
	}
}

// Ping 使用 Windows 内核 IcmpSendEcho 进行高速 ICMP 探测（免 raw socket 管理员权限）
func (n *NetworkImpl) Ping(ctx context.Context, ipStr string, timeout time.Duration) (time.Duration, bool, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return 0, false, fmt.Errorf("only IPv4 ping supported currently")
	}

	hIcmp, _, err := procIcmpCreateFile.Call()
	if hIcmp == uintptr(windows.InvalidHandle) || hIcmp == 0 {
		return 0, false, fmt.Errorf("IcmpCreateFile failed: %w", err)
	}
	defer procIcmpCloseHandle.Call(hIcmp)

	destIP := binary.LittleEndian.Uint32(ip)
	sendData := []byte("hubkit-ping")
	replyBufSize := unsafe.Sizeof(icmpEchoReply{}) + uintptr(len(sendData)) + 8
	replyBuf := make([]byte, replyBufSize)

	timeoutMs := uint32(timeout.Milliseconds())
	if timeoutMs == 0 {
		timeoutMs = 1000
	}

	ret, _, _ := procIcmpSendEcho.Call(
		hIcmp,
		uintptr(destIP),
		uintptr(unsafe.Pointer(&sendData[0])),
		uintptr(len(sendData)),
		0,
		uintptr(unsafe.Pointer(&replyBuf[0])),
		replyBufSize,
		uintptr(timeoutMs),
	)

	if ret == 0 {
		return 0, false, nil // 超时或不可达
	}

	reply := (*icmpEchoReply)(unsafe.Pointer(&replyBuf[0]))
	if reply.Status == 0 { // IP_SUCCESS
		rtt := time.Duration(reply.RoundTripTime) * time.Millisecond
		if rtt == 0 {
			rtt = time.Millisecond // 局域网内极低延迟
		}
		return rtt, true, nil
	}

	return 0, false, nil
}
