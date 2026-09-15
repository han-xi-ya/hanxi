package wsl

// 发行版取证详情（只读）：磁盘占用双口径（逻辑/实占 + 稀疏标志）、
// 注册表身份（BasePath/PFN）、运行态指标（根盘 df 用量 / IPv4）。
// 运行态指标只在确认"运行中"后才采集——对停止的发行版执行 wsl -d 会被
// 平台语义顺手拉起，"只读取证"不该有这个副作用；运行态拿不准时同样不跑。

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows 未收录 GetCompressedFileSizeW，按 platform/windows 同款 LazyProc 手法直调。
var procGetCompressedFileSizeW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// DistroForensics 单发行版取证详情载荷。
// 取不到的项一律留空并汇入 Notes 如实说明，不以零值冒充测定结果。
type DistroForensics struct {
	Name         string   `json:"name"`
	Pfn          string   `json:"pfn"` // PackageFamilyName（商店发行版才有）
	BasePath     string   `json:"basePath"`
	VhdxPath     string   `json:"vhdxPath"`
	LogicalBytes int64    `json:"logicalBytes"` // VHDX 逻辑大小
	AllocBytes   int64    `json:"allocBytes"`   // 磁盘实占（稀疏盘显著小于逻辑值）
	Sparse       bool     `json:"sparse"`
	Running      bool     `json:"running"`
	RuntimeKnown bool     `json:"runtimeKnown"` // -q --running 通道是否可用
	DfTotalMB    int64    `json:"dfTotalMB"`
	DfUsedMB     int64    `json:"dfUsedMB"`
	DfAvailMB    int64    `json:"dfAvailMB"`
	DfUsePct     int      `json:"dfUsePct"`
	DfOK         bool     `json:"dfOk"` // guest df 是否取到
	IPv4         string   `json:"ipv4"`
	NetworkMode  string   `json:"networkMode"` // 宿主 .wslconfig 网络模式（nat/bridged/mirrored/unknown）
	Notes        []string `json:"notes"`
}

// GetDistroForensics 展开行取证：名称先过实时白名单（与六操作同基线），
// 全部子项只读、单项失败不拖垮整体，缺口进 Notes。
func (s *WslService) GetDistroForensics(name string) (DistroForensics, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroForensics{}, err
	}

	f := DistroForensics{Name: name, NetworkMode: hostNetworkMode()}
	if f.NetworkMode == "mirrored" {
		f.Notes = append(f.Notes, "本机为镜像网络：发行版与 Windows 共用网络接口，IP 即宿主机 IP，本机 localhost 直通其服务")
	}

	// 1) 运行态：-q --running 名单是跨语言判据；通道不可得时宁可不采 guest 指标。
	runningSet, runningOK := s.quietSet(ctx, "--running")
	f.RuntimeKnown = runningOK
	f.Running = runningOK && runningSet[strings.ToLower(name)]

	// 2) 磁盘与注册表：Lxss 巡查补 BasePath/VHDX/PFN。
	if store, err := s.lxss(ctx); err == nil {
		if e, found := store[strings.ToLower(name)]; found {
			f.Pfn = e.Pfn
			f.BasePath = e.BasePath
			f.VhdxPath = e.Vhdx
		} else {
			f.Notes = append(f.Notes, "注册表 Lxss 未查到该发行版登记（可能是子系统的导入形态差异）")
		}
	} else {
		f.Notes = append(f.Notes, "注册表巡查失败，安装路径与磁盘占用不可得")
	}
	if f.VhdxPath != "" {
		f.LogicalBytes = fileSize(f.VhdxPath)
		alloc, sparse, serr := diskAllocStats(f.VhdxPath)
		if serr != nil {
			f.Notes = append(f.Notes, fmt.Sprintf("VHDX 实占/稀疏探测失败: %v", serr))
		} else {
			f.AllocBytes, f.Sparse = alloc, sparse
		}
	} else {
		f.Notes = append(f.Notes, "未定位到 ext4.vhdx（WSL1 或非常规布局），磁盘占用列不适用")
	}

	// 3) 运行态指标：仅运行中才进 guest（避免取证顺手拉起发行版）。
	switch {
	case !f.RuntimeKnown:
		f.Notes = append(f.Notes, "运行态通道不可得，guest 内用量与 IP 未探测（防误启动）")
	case !f.Running:
		f.Notes = append(f.Notes, "发行版未运行，guest 内用量与 IP 未探测（避免只读取证顺手启动它）")
	default:
		s.fillGuestMetrics(ctx, &f)
	}
	return f, nil
}

// fillGuestMetrics 在 guest 内跑 df 与 IP 探测；各 10s 小超时，失败只记 Notes。
func (s *WslService) fillGuestMetrics(ctx context.Context, f *DistroForensics) {
	gctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := s.runWsl(gctx, "-d", f.Name, "--", "df", "-B1M", "/"); err != nil {
		f.Notes = append(f.Notes, fmt.Sprintf("guest 用量探测失败: %v", err))
	} else if df, ok := parseDfRoot(out); ok {
		f.DfTotalMB, f.DfUsedMB, f.DfAvailMB, f.DfOK = df[0], df[1], df[2], true
		f.DfUsePct = int(df[3])
	} else {
		f.Notes = append(f.Notes, "guest 用量输出无法解析（df 格式可能已变化）")
	}

	igctx, icancel := context.WithTimeout(ctx, 10*time.Second)
	defer icancel()
	// hostname -I 是多数发行版可用的最简通道；拿不到再退 ip -o 结构化输出。
	if out, err := s.runWsl(igctx, "-d", f.Name, "--", "hostname", "-I"); err == nil {
		if ip := firstIPv4(out); ip != "" {
			f.IPv4 = ip
		}
	}
	if f.IPv4 == "" {
		if out, err := s.runWsl(igctx, "-d", f.Name, "--", "ip", "-4", "-o", "addr", "show", "scope", "global"); err == nil {
			f.IPv4 = firstIPv4(out)
		}
	}
	if f.IPv4 == "" {
		f.Notes = append(f.Notes, "未取到发行版 IPv4（NAT 模式下重启后会变化）")
	}
}

// diskAllocStats VHDX 实占字节与稀疏标志：稀疏/压缩文件的"逻辑大小"远大于
// 磁盘真实占位，磁盘瘦身决策必须看实占（GetCompressedFileSizeW 即真实簇数口径）。
func diskAllocStats(path string) (alloc int64, sparse bool, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return 0, false, fmt.Errorf("读取文件属性失败: %w", err)
	}
	sparse = attrs&windows.FILE_ATTRIBUTE_SPARSE_FILE != 0
	var high uint32
	low, _, lerr := procGetCompressedFileSizeW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&high)),
	)
	// 哨兵语义：返回 0xFFFFFFFF 时以 GetLastError 定成败（可能真大小恰好撞哨兵，此时 err=0 判成功）。
	if low == 0xFFFFFFFF && lerr != nil && lerr != windows.ERROR_SUCCESS {
		return 0, sparse, fmt.Errorf("读取实占大小失败: %w", lerr)
	}
	return int64(high)<<32 | int64(low), sparse, nil
}

// dfRootRe 匹配 `df -B1M` 的用量四元组（1M 块带 M 后缀 + 百分比）。
// 文件系统名过长会折行，因此对整段输出搜索而非按行拆分。
var dfRootRe = regexp.MustCompile(`(\d+)M\s+(\d+)M\s+(\d+)M\s+(\d+)%`)

// parseDfRoot 提取 [totalMB, usedMB, availMB, usePct]。
func parseDfRoot(out string) ([4]int64, bool) {
	m := dfRootRe.FindStringSubmatch(out)
	if m == nil {
		return [4]int64{}, false
	}
	var v [4]int64
	for i := range 4 {
		if _, err := fmt.Sscanf(m[i+1], "%d", &v[i]); err != nil {
			return [4]int64{}, false
		}
	}
	if v[0] <= 0 {
		return [4]int64{}, false
	}
	return v, true
}

// firstIPv4 从杂糅输出（空格/换行/NUL 分隔）里取第一个合法 IPv4。
// 容忍 CIDR 后缀（`ip -o` 的 inet 172.x.x.x/24）；地址列先于 brd 出现，
// 首个命中即网卡主地址。
func firstIPv4(out string) string {
	for tok := range strings.FieldsFuncSeq(out, func(r rune) bool { return r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '\x00' }) {
		if i := strings.IndexByte(tok, '/'); i >= 0 {
			tok = tok[:i]
		}
		ip := net.ParseIP(strings.TrimSpace(tok))
		if ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}
