//go:build windows

package windows

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"strings"
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

// Adapters 列举本机可用网卡与完整配置（包含网关、DNS、临时与永久 IPv6）
func (n *NetworkImpl) Adapters() ([]platform.Adapter, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("net.Interfaces failed: %w", err)
	}

	// 1. 获取系统 IPv6 临时/主地址分类映射
	ipv6Types := getWindowsIPv6Types()

	// 2. 获取各网卡绑定的网关与 DNS 配置
	gateways, dnsMap := getWindowsGatewaysAndDNS()

	result := make([]platform.Adapter, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		var ipv4List, ipv6List []string
		var ipv6Details []platform.IPv6Detail

		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				ipv4List = append(ipv4List, ip4.String())
			} else if ip6 := ipNet.IP.To16(); ip6 != nil {
				ipStr := ip6.String()
				ipv6List = append(ipv6List, ipStr)

				// 解析 IPv6 属性
				v6Type := "Public"
				isTemp := false
				if ip6.IsLinkLocalUnicast() {
					v6Type = "LinkLocal"
				} else if origin, ok := ipv6Types[ipStr]; ok && origin == "Temporary" {
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

		isLoopback := (iface.Flags & net.FlagLoopback) != 0
		isUp := (iface.Flags & net.FlagUp) != 0
		isPhysical := !isLoopback && len(iface.HardwareAddr) > 0

		gw := gateways[iface.Name]
		if gw.v4 == "" {
			gw = gateways[iface.HardwareAddr.String()]
		}

		dnsList := dnsMap[iface.Name]
		if len(dnsList) == 0 {
			dnsList = dnsMap[iface.HardwareAddr.String()]
		}

		result = append(result, platform.Adapter{
			Index:       uint32(iface.Index),
			Name:        iface.Name,
			Description: iface.Name,
			MAC:         iface.HardwareAddr.String(),
			IPv4:        ipv4List,
			IPv6:        ipv6List,
			IPv6Details: ipv6Details,
			Gateway:     gw.v4,
			IPv6Gateway: gw.v6,
			DNSServers:  dnsList,
			IsPhysical:  isPhysical,
			IsLoopback:  isLoopback,
			IsUp:        isUp,
		})
	}
	return result, nil
}

type gwPair struct {
	v4 string
	v6 string
}

// getWindowsGatewaysAndDNS 通过 netsh / PowerShell 快速读取网卡网关与 DNS 服务器
func getWindowsGatewaysAndDNS() (map[string]gwPair, map[string][]string) {
	gateways := make(map[string]gwPair)
	dnsMap := make(map[string][]string)

	// 使用 PowerShell 读取适配器网关与 DNS
	psCmd := `Get-CimInstance Win32_NetworkAdapterConfiguration -Filter 'IPEnabled=True' | ForEach-Object {
		$gws = ($_.DefaultIPGateway -join ',')
		$dns = ($_.DNSServerSearchOrder -join ',')
		Write-Output "$($_.Description)|$($_.MACAddress)|$gws|$dns"
	}`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.Output()
	if err != nil {
		return gateways, dnsMap
	}

	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.Split(l, "|")
		if len(parts) < 4 {
			continue
		}
		desc, mac, gwStr, dnsStr := parts[0], parts[1], parts[2], parts[3]

		var pair gwPair
		for _, g := range strings.Split(gwStr, ",") {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if net.ParseIP(g).To4() != nil && pair.v4 == "" {
				pair.v4 = g
			} else if pair.v6 == "" {
				pair.v6 = g
			}
		}

		var dnsList []string
		for _, d := range strings.Split(dnsStr, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				dnsList = append(dnsList, d)
			}
		}

		if mac != "" {
			macStd := strings.ToLower(strings.ReplaceAll(mac, "-", ":"))
			gateways[macStd] = pair
			dnsMap[macStd] = dnsList
		}
		if desc != "" {
			gateways[desc] = pair
			dnsMap[desc] = dnsList
		}
	}

	return gateways, dnsMap
}

// getWindowsIPv6Types 查询各 IPv6 地址的 SuffixOrigin（识别是否为临时地址）
func getWindowsIPv6Types() map[string]string {
	res := make(map[string]string)
	psCmd := `Get-NetIPAddress -AddressFamily IPv6 | ForEach-Object { Write-Output "$($_.IPAddress)|$($_.SuffixOrigin)" }`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.Output()
	if err != nil {
		return res
	}

	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.Split(l, "|")
		if len(parts) >= 2 {
			ip := strings.Split(parts[0], "%")[0] // 去掉 scope id 如 %13
			origin := parts[1]
			// SuffixOrigin 5 表示 Random (临时隐私地址)，4 表示 Link / DHCP / Manual
			if origin == "5" || strings.EqualFold(origin, "Random") {
				res[ip] = "Temporary"
			} else {
				res[ip] = "Public"
			}
		}
	}
	return res
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
