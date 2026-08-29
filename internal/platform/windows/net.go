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
	"hanxi/internal/platform"
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

// Adapters 列举本机可用网卡与完整配置（包含网关、DNS、临时与永久 IPv6）
// 优化：采用 Windows 原生 Win32 API GetAdaptersAddresses（亚毫秒级直接内存调用），彻底替代起子进程执行 PowerShell/WMI 导致的数秒延迟
func (n *NetworkImpl) Adapters() ([]platform.Adapter, error) {
	var size uint32 = 15000
	var buf []byte
	var err error

	// 循环重试以确保分配足够缓冲区
	var pAddresses *windows.IpAdapterAddresses
	flags := uint32(windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_INCLUDE_PREFIX)

	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		pAddresses = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err = windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, pAddresses, &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, fmt.Errorf("GetAdaptersAddresses failed: %w", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("GetAdaptersAddresses failed after retry: %w", err)
	}

	var result []platform.Adapter

	for curr := pAddresses; curr != nil; curr = curr.Next {
		// 网卡名称与描述
		name := windows.UTF16PtrToString(curr.FriendlyName)
		desc := windows.UTF16PtrToString(curr.Description)
		if name == "" {
			name = windows.BytePtrToString(curr.AdapterName)
		}
		if desc == "" {
			desc = name
		}

		// MAC 地址
		var macStr string
		if curr.PhysicalAddressLength > 0 && curr.PhysicalAddressLength <= 8 {
			macBytes := curr.PhysicalAddress[:curr.PhysicalAddressLength]
			macStr = net.HardwareAddr(macBytes).String()
		}

		// 单播 IP 列表 (IPv4 / IPv6)
		var ipv4List, ipv6List []string
		var ipv6Details []platform.IPv6Detail

		for u := curr.FirstUnicastAddress; u != nil; u = u.Next {
			ip := u.Address.IP()
			if ip == nil {
				continue
			}

			if ip4 := ip.To4(); ip4 != nil {
				ipv4List = append(ipv4List, ip4.String())
			} else if ip6 := ip.To16(); ip6 != nil {
				ipStr := ip6.String()
				ipv6List = append(ipv6List, ipStr)

				v6Type := "Public"
				isTemp := false
				if ip6.IsLinkLocalUnicast() {
					v6Type = "LinkLocal"
				} else if u.SuffixOrigin == windows.IpSuffixOriginRandom {
					v6Type = "Temporary"
					isTemp = true
				}

				ipv6Details = append(ipv6Details, platform.IPv6Detail{
					Address:     ipStr,
					Type:        v6Type,
					IsTemporary: isTemp,
				})
			}
		}

		// 网关列表 (IPv4 / IPv6)
		var gwV4, gwV6 string
		for g := curr.FirstGatewayAddress; g != nil; g = g.Next {
			ip := g.Address.IP()
			if ip == nil {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil && gwV4 == "" {
				gwV4 = ip4.String()
			} else if ip6 := ip.To16(); ip6 != nil && ip.To4() == nil && gwV6 == "" {
				gwV6 = ip6.String()
			}
		}

		// DNS 服务器列表
		var dnsList []string
		for d := curr.FirstDnsServerAddress; d != nil; d = d.Next {
			ip := d.Address.IP()
			if ip != nil {
				dnsList = append(dnsList, ip.String())
			}
		}

		isLoopback := curr.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK
		isPhysical := (curr.IfType == windows.IF_TYPE_ETHERNET_CSMACD || curr.IfType == windows.IF_TYPE_IEEE80211) && !isLoopback
		isUp := curr.OperStatus == windows.IfOperStatusUp

		result = append(result, platform.Adapter{
			Index:       curr.IfIndex,
			Name:        name,
			Description: desc,
			MAC:         macStr,
			IPv4:        ipv4List,
			IPv6:        ipv6List,
			IPv6Details: ipv6Details,
			Gateway:     gwV4,
			IPv6Gateway: gwV6,
			DNSServers:  dnsList,
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
	sendData := []byte("hanxi-ping")
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
