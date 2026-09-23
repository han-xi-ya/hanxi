//go:build windows

// collect_windows.go Windows 采集实现：注册表 + WinAPI + 标准库，全部只读。
// 每段采集独立降级（失败记入 Report.Errors，不阻断整表），字段级空值即事实边界。
package sysinfo

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modKernel32SI = syscall.NewLazyDLL("kernel32.dll")
	modUser32SI   = syscall.NewLazyDLL("user32.dll")

	procGetTickCount64          = modKernel32SI.NewProc("GetTickCount64")
	procGlobalMemoryStatusEx    = modKernel32SI.NewProc("GlobalMemoryStatusEx")
	procGetLogicalProcessorInfo = modKernel32SI.NewProc("GetLogicalProcessorInformation")
	procEnumDisplayDevicesW     = modUser32SI.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettingsW    = modUser32SI.NewProc("EnumDisplaySettingsW")
	procGetSystemMetrics        = modUser32SI.NewProc("GetSystemMetrics")
)

const (
	EDDS_ACTIVE           = 0x00000001 // DISPLAY_DEVICE_ATTACHED_TO_DESKTOP
	EDDS_PRIMARY          = 0x00000002 // DISPLAY_DEVICE_PRIMARY_DEVICE
	enumCurrentSettings   = 0xFFFFFFFF // ENUM_CURRENT_SETTINGS
	smCXScreen            = 0
	smCYScreen            = 1
	relationProcessorCore = 0
)

func collectMachine() (MachineInfo, error) {
	var m MachineInfo
	m.Hostname, _ = os.Hostname()
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return m, fmt.Errorf("打开 BIOS 描述键: %w", err)
	}
	defer k.Close()
	m.Manufacturer, _, _ = k.GetStringValue("SystemManufacturer")
	m.Model, _, _ = k.GetStringValue("SystemProductName")
	m.BIOSVendor, _, _ = k.GetStringValue("BIOSVendor")
	m.BIOSVersion, _, _ = k.GetStringValue("BIOSVersion")
	m.SystemSKU, _, _ = k.GetStringValue("SystemSku")
	if m.Model == "" { // 组装机 SKU/型号常缺，主板回落
		if base, berr := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BaseBoard`, registry.QUERY_VALUE); berr == nil {
			m.Model, _, _ = base.GetStringValue("Product")
			base.Close()
		}
	}
	return m, nil
}

func collectOS() (OSInfo, error) {
	var o OSInfo
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return o, fmt.Errorf("打开系统版本键: %w", err)
	}
	defer k.Close()
	name, _, _ := k.GetStringValue("ProductName")
	build, _, _ := k.GetStringValue("CurrentBuildNumber")
	if lab, _, lerr := k.GetStringValue("BuildLabEx"); lerr == nil && lab != "" {
		build = strings.TrimSuffix(lab, ".amd64fre") // BuildLabEx 带 UBR：22631.4317
		o.Build = firstTwoSegments(build)
	} else {
		o.Build = build
	}
	o.Edition, _, _ = k.GetStringValue("EditionID")
	o.Version, _, _ = k.GetStringValue("DisplayVersion")
	if o.Version == "" {
		o.Version, _, _ = k.GetStringValue("ReleaseId")
	}
	if ts, _, terr := k.GetIntegerValue("InstallDate"); terr == nil {
		o.InstallDate = time.Unix(int64(ts), 0).Format("2006-01-02")
	}
	o.ProductName = normalizeProductName(name, build)
	o.Is64Bit = runtime.GOARCH == "amd64" || isWow64Self()
	tick, _, _ := procGetTickCount64.Call()
	o.UptimeSeconds = int64(tick) / 1000
	o.Uptime = formatUptime(o.UptimeSeconds)
	return o, nil
}

// normalizeProductName 注册表已知事实：Win11 各版本仍写 "Windows 10 ..."，
// 以构建号（≥22000 即 11）规一显示名；除此之外原样透出（不猜不造）。
func normalizeProductName(registryName, build string) string {
	num := majorBuild(build)
	if strings.Contains(registryName, "Windows 10") && num >= 22000 {
		return strings.Replace(registryName, "Windows 10", "Windows 11", 1)
	}
	return registryName
}

// majorBuild 取构建号主段（"22631.4317"→22631；非数字→0）。
func majorBuild(build string) int {
	base := strings.SplitN(strings.TrimSpace(build), ".", 2)[0]
	n := 0
	for _, c := range base {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func firstTwoSegments(lab string) string {
	parts := strings.Split(lab, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return lab
}

func isWow64Self() bool {
	var wow bool
	if err := windows.IsWow64Process(windows.CurrentProcess(), &wow); err == nil {
		return wow
	}
	return false
}

func collectCPU() (CPUInfo, error) {
	var c CPUInfo
	c.Logical = runtime.NumCPU()
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err == nil {
		c.Name, _, _ = k.GetStringValue("ProcessorNameString")
		c.Name = strings.TrimSpace(c.Name)
		c.Vendor, _, _ = k.GetStringValue("VendorIdentifier")
		if mhz, _, merr := k.GetIntegerValue("~MHz"); merr == nil {
			c.SpeedMHz = int(mhz)
		}
		k.Close()
	}
	if sk, kerr := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor`, registry.READ); kerr == nil {
		n, _ := sk.ReadSubKeyNames(-1)
		sk.Close()
		c.Sockets = len(n)
	}
	if cores, logical, cerr := countCoresViaLPInfo(); cerr == nil {
		if cores > 0 {
			c.Cores = cores
		}
		if logical > 0 {
			c.Logical = logical
		}
	}
	return c, err
}

// lpiRecord SYSTEM_LOGICAL_PROCESSOR_INFORMATION（v1，ULONG64 掩码）：
// Relationship DWORD + pad DWORD + GROUP_AFFINITY{Group WORD, pad, Mask ULONGLONG}。
type lpiRecord struct {
	Relationship uint32
	_pad0        uint32
	Group        uint16
	_pad1        [6]byte
	Mask         uint64
}

func countCoresViaLPInfo() (cores, logical int, err error) {
	var size uint32
	// 首探缓冲区大小（FALSE+ERROR_INSUFFICIENT_BUFFER 为预期路径）
	if r, _, e := procGetLogicalProcessorInfo.Call(0, 0, uintptr(unsafe.Pointer(&size))); r == 0 && size == 0 {
		return 0, 0, fmt.Errorf("GetLogicalProcessorInformation 探测失败: %w", e)
	}
	buf := make([]byte, size)
	r, _, e := procGetLogicalProcessorInfo.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return 0, 0, fmt.Errorf("GetLogicalProcessorInformation: %w", e)
	}
	n := int(size) / int(unsafe.Sizeof(lpiRecord{}))
	for i := 0; i < n; i++ {
		rec := (*lpiRecord)(unsafe.Pointer(&buf[i*int(unsafe.Sizeof(lpiRecord{}))]))
		if rec.Relationship == relationProcessorCore {
			cores++
			logical += popcountU64(rec.Mask)
		}
	}
	return cores, logical, nil
}

func popcountU64(v uint64) int {
	cnt := 0
	for v != 0 {
		v &= v - 1
		cnt++
	}
	return cnt
}

// memoryStatusEx MEMORYSTATUSEX（x/sys 无该原语包装，本地声明全形；
// dwLength 必须先行置位，否则 API 拒收）。
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func collectMemory() (MemoryInfo, error) {
	var st memoryStatusEx
	st.Length = uint32(unsafe.Sizeof(st))
	if r, _, e := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return MemoryInfo{}, fmt.Errorf("GlobalMemoryStatusEx: %w", e)
	}
	return MemoryInfo{
		TotalBytes:     st.TotalPhys,
		AvailableBytes: st.AvailPhys,
		LoadPercent:    st.MemoryLoad,
		CommitTotal:    st.TotalPageFile - st.AvailPageFile,
		CommitLimit:    st.TotalPageFile,
	}, nil
}

// displayDeviceW DISPLAY_DEVICEW。
type displayDeviceW struct {
	Size       uint32
	DeviceName [32]uint16
	DeviceStr  [128]uint16
	StateFlags uint32
	DeviceID   [128]uint16
	DevKey     [128]uint16
}

// devModeW DEVMODEW（220 字节全形；本包仅消费前段显示模式字段）。
type devModeW struct {
	DeviceName       [32]uint16
	DriverVersion    uint16
	Size             uint16 // DEVMODEW.dwSize：WORD，必须逐字等于 220，否则 API 直接拒收
	DriverExtra      uint32
	Fields           uint32
	LogPixels        uint32
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	_                [120]byte // 其余字段本包不消费；补齐官方 sizeof(DEVMODEW)=220
}

func collectDisplays() ([]DisplayInfo, error) {
	var out []DisplayInfo
	for i := uint32(0); i < 16; i++ { // 物理显示器数量硬上限防御
		var dev displayDeviceW
		dev.Size = uint32(unsafe.Sizeof(dev))
		r, _, _ := procEnumDisplayDevicesW.Call(0, uintptr(i), uintptr(unsafe.Pointer(&dev)), 0)
		if r == 0 {
			break
		}
		if dev.StateFlags&EDDS_ACTIVE == 0 {
			continue
		}
		info := DisplayInfo{
			Name:    windows.UTF16ToString(dev.DeviceName[:]),
			Primary: dev.StateFlags&EDDS_PRIMARY != 0,
		}
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		r2, _, _ := procEnumDisplaySettingsW.Call(uintptr(unsafe.Pointer(&dev.DeviceName[0])), enumCurrentSettings, uintptr(unsafe.Pointer(&dm)))
		if r2 != 0 {
			info.Width = int32(dm.PelsWidth)
			info.Height = int32(dm.PelsHeight)
			info.RefreshHz = int32(dm.DisplayFrequency)
			info.ColorBits = int32(dm.BitsPerPel)
		} else if info.Primary {
			w, _, _ := procGetSystemMetrics.Call(smCXScreen)
			h, _, _ := procGetSystemMetrics.Call(smCYScreen)
			info.Width, info.Height = int32(w), int32(h)
		}
		out = append(out, info)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未发现活动显示器")
	}
	return out, nil
}

func collectGPUs() ([]GPUInfo, error) {
	const classKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, classKey, registry.READ)
	if err != nil {
		return nil, fmt.Errorf("打开显示设备类键: %w", err)
	}
	defer root.Close()
	subs, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []GPUInfo
	for _, sub := range subs {
		if !strings.HasPrefix(sub, "0") || len(sub) != 4 { // 实例键恒 0000/0001…；跳过 Properties 等
			continue
		}
		k, kerr := registry.OpenKey(root, sub, registry.QUERY_VALUE)
		if kerr != nil {
			continue
		}
		g := GPUInfo{}
		g.Desc, _, _ = k.GetStringValue("DriverDesc")
		g.Provider, _, _ = k.GetStringValue("ProviderName")
		g.DriverVersion, _, _ = k.GetStringValue("DriverVersion")
		if ft, _, ferr := k.GetIntegerValue("DriverDate"); ferr == nil && ft > 0 {
			g.DriverDate = time.Unix(0, (int64(ft)-116444736000000000)*100).Format("2006-01-02")
		}
		k.Close()
		if g.Desc != "" {
			out = append(out, g)
		}
	}
	return out, nil
}

func collectVolumes() ([]VolumeInfo, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, fmt.Errorf("GetLogicalDrives: %w", err)
	}
	var out []VolumeInfo
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":"
		root := letter + `\`
		root16, _ := windows.UTF16PtrFromString(root)
		var v VolumeInfo
		v.Letter = letter
		switch windows.GetDriveType(root16) {
		case windows.DRIVE_FIXED:
			v.Type = "fixed"
		case windows.DRIVE_REMOVABLE:
			v.Type = "removable"
		case windows.DRIVE_REMOTE:
			v.Type = "network"
		case windows.DRIVE_CDROM:
			v.Type = "cdrom"
		case windows.DRIVE_RAMDISK:
			v.Type = "ramdisk"
		default:
			v.Type = "unknown"
		}
		var free, total, user uint64
		if ferr := windows.GetDiskFreeSpaceEx(root16, &free, &total, &user); ferr == nil {
			v.TotalBytes, v.FreeBytes = total, free
		}
		var nameBuf [261]uint16
		var fsBuf [261]uint16
		if verr := windows.GetVolumeInformation(root16, &nameBuf[0], uint32(len(nameBuf)), nil, nil, nil, &fsBuf[0], uint32(len(fsBuf))); verr == nil {
			v.Label = windows.UTF16ToString(nameBuf[:])
			v.FileSystem = windows.UTF16ToString(fsBuf[:])
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未发现逻辑卷")
	}
	return out, nil
}

func collectNetwork() ([]NetInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []NetInfo
	for _, ifi := range ifaces {
		n := NetInfo{
			Name:     ifi.Name,
			MAC:      ifi.HardwareAddr.String(),
			MTU:      ifi.MTU,
			Up:       ifi.Flags&net.FlagUp != 0,
			Loopback: ifi.Flags&net.FlagLoopback != 0,
		}
		if addrs, aerr := ifi.Addrs(); aerr == nil {
			for _, a := range addrs {
				n.Addresses = append(n.Addresses, strings.SplitN(a.String(), "/", 2)[0])
			}
		}
		out = append(out, n)
	}
	return out, nil
}

// formatUptime 秒 → "N天 N小时 N分钟"（<1 分钟显示秒，全档人类可读）。
func formatUptime(sec int64) string {
	if sec < 0 {
		return ""
	}
	d, h, m := sec/86400, sec%86400/3600, sec%3600/60
	switch {
	case d > 0:
		return fmt.Sprintf("%d天%d小时%d分钟", d, h, m)
	case h > 0:
		return fmt.Sprintf("%d小时%d分钟", h, m)
	case m > 0:
		return fmt.Sprintf("%d分钟", m)
	default:
		return fmt.Sprintf("%d秒", sec)
	}
}
