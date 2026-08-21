package lan

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/platform"
	"hubkit/internal/settings"
)

// DeviceInfo 局域网活跃设备信息
type DeviceInfo struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Hostname  string `json:"hostname"`
	Remark    string `json:"remark"` // 用户自定义备注
	RTTMs     int64  `json:"rttMs"`
	IsSelf    bool   `json:"isSelf"`
	IsGateway bool   `json:"isGateway"`
}

// SubnetInfo 候选网卡/子网信息
type SubnetInfo struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

// LanProgress 实时扫描进度
type LanProgress struct {
	Scanned int `json:"scanned"`
	Total   int `json:"total"`
	Found   int `json:"found"`
}

type LanService struct {
	plat       platform.Platform
	store      *settings.Store
	cancelScan context.CancelFunc
	scanning   atomic.Bool
	mu         sync.Mutex
}

func NewLanService(plat platform.Platform, store *settings.Store) *LanService {
	return &LanService{
		plat:  plat,
		store: store,
	}
}

// GetSubnets 列出所有可用于扫描的候选网卡子网，支持常见掩码计算
func (s *LanService) GetSubnets() ([]SubnetInfo, error) {
	adapters, err := s.plat.Network().Adapters()
	if err != nil {
		return nil, fmt.Errorf("failed to get network adapters: %w", err)
	}

	var subnets []SubnetInfo
	for _, a := range adapters {
		if !a.IsUp || a.IsLoopback {
			continue
		}
		for _, ipStr := range a.IPv4 {
			ip := net.ParseIP(ipStr).To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}

			// Tailscale / CGNAT 专网 (100.64.0.0/10) 或常规局域网
			mask := net.CIDRMask(24, 32)
			if ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 {
				// Tailscale 默认也是提供 /24 快捷段，但用户可自定义更大段
				mask = net.CIDRMask(24, 32)
			}

			ipNet := net.IPNet{IP: ip.Mask(mask), Mask: mask}

			subnets = append(subnets, SubnetInfo{
				Name:    a.Name,
				IP:      ipStr,
				CIDR:    ipNet.String(),
				Gateway: a.Gateway,
			})
		}
	}
	return subnets, nil
}

// SetRemark 保存 IP 或 MAC 的用户自定义备注
func (s *LanService) SetRemark(key, remark string) error {
	if s.store == nil {
		return nil
	}
	return s.store.SetLanRemark(key, remark)
}

// parseTargets 解析 CIDR 或者 IP 范围（如 192.168.1.0/24，100.111.18.0/24，100.94.178.1-100.94.178.254）
func parseTargets(targetInput string) ([]string, error) {
	targetInput = strings.TrimSpace(targetInput)
	if targetInput == "" {
		return nil, fmt.Errorf("target range cannot be empty")
	}

	// 1. 如果包含 '-'（范围模式：例如 100.94.178.1-100.94.178.100 或 100.94.178.1-100）
	if strings.Contains(targetInput, "-") {
		parts := strings.Split(targetInput, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range format")
		}
		startIPStr := strings.TrimSpace(parts[0])
		endIPStr := strings.TrimSpace(parts[1])

		startIP := net.ParseIP(startIPStr).To4()
		if startIP == nil {
			return nil, fmt.Errorf("invalid start IP %q", startIPStr)
		}

		var endIP net.IP
		// 如果 end 仅为数字（如 192.168.1.10-50）
		if !strings.Contains(endIPStr, ".") {
			var lastByte int
			if _, err := fmt.Sscanf(endIPStr, "%d", &lastByte); err == nil && lastByte >= 0 && lastByte <= 255 {
				endIP = net.IPv4(startIP[0], startIP[1], startIP[2], byte(lastByte)).To4()
			}
		} else {
			endIP = net.ParseIP(endIPStr).To4()
		}

		if endIP == nil {
			return nil, fmt.Errorf("invalid end IP %q", endIPStr)
		}

		startInt := binary.BigEndian.Uint32(startIP)
		endInt := binary.BigEndian.Uint32(endIP)
		if startInt > endInt {
			startInt, endInt = endInt, startInt
		}

		count := endInt - startInt + 1
		if count > 4096 {
			return nil, fmt.Errorf("ip range too large (max 4096 addresses per scan)")
		}

		targets := make([]string, 0, count)
		for val := startInt; val <= endInt; val++ {
			tmp := make(net.IP, 4)
			binary.BigEndian.PutUint32(tmp, val)
			targets = append(targets, tmp.String())
		}
		return targets, nil
	}

	// 2. 如果不含 '/'，当单个 IP 或自动补全 /24 处理
	if !strings.Contains(targetInput, "/") {
		parsed := net.ParseIP(targetInput).To4()
		if parsed != nil {
			// 如果直接填入单 IP，扫描该 IP 所在网段的 /24
			targetInput = fmt.Sprintf("%d.%d.%d.0/24", parsed[0], parsed[1], parsed[2])
		} else {
			targetInput = targetInput + "/24"
		}
	}

	ip, ipNet, err := net.ParseCIDR(targetInput)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", targetInput, err)
	}

	baseIP := ip.Mask(ipNet.Mask).To4()
	if baseIP == nil {
		return nil, fmt.Errorf("only IPv4 is supported")
	}

	ones, bits := ipNet.Mask.Size()
	// 安全限制：最多支持 /20（4096 台主机）
	if ones < 20 {
		return nil, fmt.Errorf("CIDR mask /%d is too large (max /20 = 4096 hosts)", ones)
	}

	numHosts := 1 << (bits - ones)
	targets := make([]string, 0, numHosts)
	baseInt := binary.BigEndian.Uint32(baseIP)

	for i := 0; i < numHosts; i++ {
		curInt := baseInt + uint32(i)
		tmp := make(net.IP, 4)
		binary.BigEndian.PutUint32(tmp, curInt)
		// 排除网络地址与广播地址（若规模 >= /24）
		if numHosts >= 4 && (i == 0 || i == numHosts-1) {
			continue
		}
		targets = append(targets, tmp.String())
	}

	return targets, nil
}

// Scan 执行并发网段/IP范围扫描
func (s *LanService) Scan(targetRange string) ([]DeviceInfo, error) {
	if s.scanning.Swap(true) {
		return nil, fmt.Errorf("scan already in progress")
	}
	defer s.scanning.Store(false)

	targets, err := parseTargets(targetRange)
	if err != nil {
		return nil, err
	}

	// 动态超时：根据扫描数量动态调整（254个IP 10秒，更多IP适当延长，上限 30 秒）
	timeout := 10 * time.Second
	if len(targets) > 512 {
		timeout = 25 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	s.mu.Lock()
	s.cancelScan = cancel
	s.mu.Unlock()

	defer cancel()

	// 读取已保存的备注表
	var remarks map[string]string
	if s.store != nil {
		remarks = s.store.GetLanRemarks()
	}

	// 1. 预先拉取系统 ARP 邻居表
	neighbors, _ := s.plat.Network().NeighborTable()
	macMap := make(map[string]string)
	for _, n := range neighbors {
		if n.MAC != "" {
			macMap[n.IP] = n.MAC
		}
	}

	// 2. 获取本机全部 IP 与对应的物理网卡 MAC
	selfIPs := make(map[string]bool)
	adapters, _ := s.plat.Network().Adapters()
	for _, a := range adapters {
		for _, sip := range a.IPv4 {
			selfIPs[sip] = true
			if a.MAC != "" {
				macMap[sip] = a.MAC
			}
		}
	}

	total := len(targets)
	var scannedCount int64
	var foundDevices []DeviceInfo
	var devMu sync.Mutex

	// 并发 Worker 池 (根据数量提升至 64 并发)
	concurrency := 64
	if total < concurrency {
		concurrency = total
	}
	ipChan := make(chan string, total)
	for _, t := range targets {
		ipChan <- t
	}
	close(ipChan)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range ipChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// 针对跨子网或 VPN，ICMP 超时设为 1200ms
				rtt, ok, _ := s.plat.Network().Ping(ctx, target, 1200*time.Millisecond)

				curScanned := atomic.AddInt64(&scannedCount, 1)

				if ok {
					mac := macMap[target]
					remark := remarks[target]
					if remark == "" && mac != "" {
						remark = remarks[mac]
					}

					dev := DeviceInfo{
						IP:     target,
						MAC:    mac,
						Remark: remark,
						RTTMs:  rtt.Milliseconds(),
						IsSelf: selfIPs[target],
					}
					if dev.RTTMs == 0 {
						dev.RTTMs = 1
					}

					devMu.Lock()
					foundDevices = append(foundDevices, dev)
					foundLen := len(foundDevices)
					devMu.Unlock()

					// 推送进度
					if app := application.Get(); app != nil && app.Event != nil {
						app.Event.Emit("lan:progress", LanProgress{
							Scanned: int(curScanned),
							Total:   total,
							Found:   foundLen,
						})
					}
				} else {
					if curScanned%10 == 0 || curScanned == int64(total) {
						devMu.Lock()
						foundLen := len(foundDevices)
						devMu.Unlock()
						if app := application.Get(); app != nil && app.Event != nil {
							app.Event.Emit("lan:progress", LanProgress{
								Scanned: int(curScanned),
								Total:   total,
								Found:   foundLen,
							})
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	// 扫描完成后再拉取一次最新的 ARP 表补全 MAC 并挂载备注
	if latestNeighbors, err := s.plat.Network().NeighborTable(); err == nil {
		for _, n := range latestNeighbors {
			if n.MAC != "" {
				macMap[n.IP] = n.MAC
			}
		}
		for i := range foundDevices {
			if foundDevices[i].MAC == "" {
				foundDevices[i].MAC = macMap[foundDevices[i].IP]
			}
			if foundDevices[i].Remark == "" {
				if r := remarks[foundDevices[i].IP]; r != "" {
					foundDevices[i].Remark = r
				} else if r := remarks[foundDevices[i].MAC]; r != "" {
					foundDevices[i].Remark = r
				}
			}
		}
	}

	// 排序：按 IPv4 数值升序排列
	sort.Slice(foundDevices, func(i, j int) bool {
		ip1 := net.ParseIP(foundDevices[i].IP).To4()
		ip2 := net.ParseIP(foundDevices[j].IP).To4()
		if ip1 != nil && ip2 != nil {
			return binary.BigEndian.Uint32(ip1) < binary.BigEndian.Uint32(ip2)
		}
		return foundDevices[i].IP < foundDevices[j].IP
	})

	return foundDevices, nil
}

// Cancel 取消当前正在进行的扫描
func (s *LanService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelScan != nil {
		s.cancelScan()
		s.cancelScan = nil
	}
}
