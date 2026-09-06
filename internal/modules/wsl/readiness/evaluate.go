package readiness

import (
	"fmt"
	"regexp"
	"strings"
)

var supportedArchRe = regexp.MustCompile(`(?i)^(AMD64|X86_64|ARM64)$`)

// 体检项构建按数据源分相（流式推送的骨架），前端 pending 清单与
// readinessItems 的 key 集合有回归锁互锁，两边改一边必须同步。

// SystemItems 来自只读探针（注册表/WMI/Appx），秒级返回。
func SystemItems(p ProbeResult) []CheckItem {
	var items []CheckItem

	// 1. 系统版本
	buildOK := p.OSBuild >= 19041
	items = append(items, CheckItem{
		Key: "build", Label: "系统版本", State: boolState(buildOK),
		Value:  fmt.Sprintf("Build %d.%d", p.OSBuild, p.OSUBR),
		Detail: boolText(buildOK, "满足 WSL2 门槛（要求 ≥ 19041 / Win10 2004+）", "不满足：需升级到 Windows 10 2004 或更高版本"),
	})

	// 2. CPU 架构
	archOK := supportedArchRe.MatchString(p.Arch)
	items = append(items, CheckItem{
		Key: "arch", Label: "CPU 架构", State: boolState(archOK),
		Value:  p.Arch,
		Detail: boolText(archOK, "WSL2 官方支持 x64 与 ARM64", "非 x64 / ARM64，不受官方支持"),
	})

	// 3. CPU 虚拟化。监控程序已在运行时固件位读不到真值（WMI 语义），
	// 以 HypervisorPresent 为第一判据——这本身就是"虚拟化工作正常"的最强证据。
	virtState, virtValue, virtDetail := StateBad, "未在固件开启", "BIOS/UEFI 中开启 VT-x / AMD-V（或 SVM Mode）后重试"
	switch {
	case p.Hypervisor:
		virtState, virtValue, virtDetail = StateOK, "监控程序运行中", "虚拟机监控程序已在运行，虚拟化链路工作正常"
	case p.VTFirmware == nil:
		virtState, virtValue, virtDetail = StateInfo, "未知", "读取不到固件虚拟化状态，若安装后发行版无法启动请检查 BIOS"
	case *p.VTFirmware:
		virtState, virtValue, virtDetail = StateOK, "固件已开启", "BIOS 中 VT-x/AMD-V 已启用"
	}
	items = append(items, CheckItem{Key: "virt", Label: "CPU 虚拟化 (VT-x / AMD-V)", State: virtState, Value: virtValue, Detail: virtDetail})

	// 4. 虚拟机监控程序。注意：内核隔离（HVCI/VBS）也会拉起监控程序——
	// 它在场不代表虚拟机平台开着（实机校准：功能双双禁用时 HypervisorPresent 仍为 true），
	// WSL2 的硬前提是下一项的可选功能开关。
	items = append(items, CheckItem{
		Key: "hypervisor", Label: "虚拟机监控程序", State: boolState(p.Hypervisor),
		Value:  boolText(p.Hypervisor, "正在运行", "未运行"),
		Detail: boolText(p.Hypervisor, "可能来自内核隔离（HVCI），是否能承载 WSL2 看下一项「可选功能」", "未运行时「一键开启」会自动启用虚拟机平台功能（需重启）"),
	})

	// 5. 可选功能开关（vmcompute/LxssManager 服务存在性判据）：WSL2 真正的门槛。
	items = append(items, evaluateFeature(p))

	// 6. VBS 仅为信息项（开启与否都不阻塞 WSL2）。
	vbsValue, vbsDetail := "未知", "读取失败不影响 WSL2 使用"
	if p.VBS != nil {
		switch *p.VBS {
		case 2:
			vbsValue, vbsDetail = "运行中", "内核隔离/内存完整性开启，与 WSL2 完全兼容"
		case 1:
			vbsValue, vbsDetail = "可用未启用", "不影响 WSL2 使用"
		default:
			vbsValue, vbsDetail = "未启用", "不影响 WSL2 使用"
		}
	}
	items = append(items, CheckItem{Key: "vbs", Label: "基于虚拟化的安全性 (VBS)", State: StateInfo, Value: vbsValue, Detail: vbsDetail})

	// 7. Microsoft Store
	items = append(items, CheckItem{
		Key: "store", Label: "Microsoft Store", State: boolState(p.Store),
		Value:  boolText(p.Store, "已安装", "未检测到"),
		Detail: boolText(p.Store, "商店渠道可用于发行版分发", "商店缺失时可用 MSI 离线包 + wsl --import 方案"),
	})

	// 8. 安装形态：MSIX / MSI / 系统启动器三路信号 × 运行时版本互证。
	// 专治"设置只显一个、卸了另一个还活着"（本机 2.7.13 双形态共存实证）；
	// System32\wsl.exe 文件版本跟随 OS 构建（10.0.x），只作存在性信号、不参与版本比对。
	items = append(items, evaluateForm(p))
	return items
}

// evaluateFeature 可选功能开关体检：虚拟机平台是 WSL2 的硬前提。
// 判据为 Win32_OptionalFeature WMI（免管理员，DISM 真值对照校准）；
// 未启用定级 warn 而非 info——"监控程序在跑"给不了安全感，必须让结论条点名。
func evaluateFeature(p ProbeResult) CheckItem {
	item := CheckItem{Key: "feature", Label: "可选功能 (虚拟机平台)"}
	if p.FeatureVM {
		legacy := "关"
		if p.FeatureWSL {
			legacy = "开"
		}
		item.State = StateOK
		item.Value = "虚拟机平台 已启用 · 旧版WSL " + legacy
		switch {
		case p.RebootPending:
			item.Value += " · 待重启生效"
			item.Detail = "功能变更已记入系统台账——重启一次后完整加载（在此之前发行版无法拉起）；重启后重新体检即恢复干净通过"
		case !p.Hypervisor:
			item.Detail = "已启用但虚拟机监控程序尚未运行——重启一次后发行版才能拉起"
		default:
			item.Detail = "WSL2 的硬前提已就位并可承载发行版"
		}
		return item
	}
	item.State = StateWarn
	item.Value = "虚拟机平台 未启用"
	item.Detail = "WSL2 的硬前提缺失：用「🚀 一键开启」或「▶️ 开启虚拟机平台」启用（UAC 提权，重启生效）。即使「虚拟机监控程序」在运行，那也来自内核隔离（HVCI），不能替代本功能承载 WSL2。"
	return item
}

// RuntimeItem 来自本机 wsl.exe 运行时。
func RuntimeItem(wslVersion string) CheckItem {
	if wslVersion != "" {
		return CheckItem{Key: "wsl", Label: "WSL 本体", State: StateOK, Value: wslVersion, Detail: "已安装，可通过「检查更新」管理版本"}
	}
	return CheckItem{Key: "wsl", Label: "WSL 本体", State: StateInfo, Value: "未安装", Detail: "点「🚀 一键开启」安装，或到「版本与发行版」页下载 MSI 离线包"}
}

// NetworkItem 来自 Go 原生 GitHub 通道探测（403 = 「已禁止(403)」根因）。
// 通道类异常一律 warn 不进 bad：硬阻塞只留给系统版本/架构/虚拟化这类
// "怎么都绕不过去"的门槛——网络有代理与离线导入两条绕行路。
func NetworkItem(api, gh any) CheckItem {
	netState, netDetail := StateWarn, "GitHub 不可达：在线安装不可用；可挂代理，或走完全离线的 MSI / rootfs 导入方案"
	switch {
	case netStatus(api) == "200":
		netState, netDetail = StateOK, "一键安装通道畅通"
	case netStatus(api) == "403":
		netState, netDetail = StateWarn, "GitHub API 被拦截——正是 wsl --install 报「已禁止(403)」的根因：换网络/挂代理，或改用「版本与发行版」页的 MSI 直链离线安装"
	}
	return CheckItem{Key: "net", Label: "GitHub 安装通道", State: netState, Value: "API " + netStatus(api) + " · Releases " + netStatus(gh), Detail: netDetail}
}

// BuildItems 全量体检项（固定顺序，与前端 pending 骨架一致）。
func BuildItems(p ProbeResult, wslVersion string) []CheckItem {
	items := SystemItems(p)
	items = append(items, NetworkItem(p.APIGitHub, p.GitHub), RuntimeItem(wslVersion))
	return items
}

// Evaluate 汇总结论：探针快照 + 运行时版本 + 发行版现状 → 完整报告（纯函数）。
func Evaluate(p ProbeResult, wslVersion string, distros []Distro, collectedAt string) Report {
	items := BuildItems(p, wslVersion)
	// 跨源互证（"卸了但没卸干净"）：运行时失联却有安装形态残留 → 升级 WSL 项为 warn。
	if wslVersion == "" && (p.WslMSIX != "" || p.WslMSI != "") {
		for i := range items {
			if items[i].Key == "wsl" {
				items[i].State = StateWarn
				items[i].Detail = "wsl.exe 运行时已不可用，但仍检出安装形态残留——卸载未完成；可重装继续使用，或执行「卸载 WSL」正规清理"
			}
		}
	}
	verdict, title, detail := verdictOf(items, wslVersion)
	return Report{
		Verdict: verdict, VerdictTitle: title, VerdictDetail: detail,
		Checks: items, WslVersion: wslVersion, Distros: distros, CollectedAt: collectedAt,
		VMPlatformEnabled: p.FeatureVM, MachineArch: NormalizeMachineArch(p.Arch),
		RebootPending: p.RebootPending,
	}
}

// NormalizeMachineArch 把 PROCESSOR_ARCHITECTURE 归一到 WSL MSI 资产的平台口径。
func NormalizeMachineArch(arch string) string {
	switch strings.ToUpper(strings.TrimSpace(arch)) {
	case "AMD64", "X86_64":
		return "x64"
	case "ARM64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}

// verdictOf 总体结论：硬阻塞 > 注意项 > 就绪。
func verdictOf(items []CheckItem, wslVersion string) (verdict, title, detail string) {
	var blockers, warns []CheckItem
	for _, c := range items {
		switch c.State {
		case StateBad:
			blockers = append(blockers, c)
		case StateWarn:
			warns = append(warns, c)
		}
	}
	switch {
	case len(blockers) > 0:
		return VerdictBlocked,
			"无法开启 WSL：" + strings.Join(labelAll(blockers), "、"),
			"先解决红色项（" + blockers[0].Detail + "），其余见逐项结论。"
	case len(warns) > 0:
		if wslVersion != "" {
			title = "WSL2 可正常使用，但有注意项"
		} else {
			title = "硬件与系统满足，可以安装 WSL2——但有注意项"
		}
		return VerdictAttention, title, warns[0].Detail
	default:
		if wslVersion != "" {
			return VerdictReady,
				fmt.Sprintf("WSL2 运行正常（%s）", wslVersion),
				"无阻塞项。缺发行版时到「版本与发行版」页从官方清单一键安装。"
		}
		return VerdictReady,
			"完全满足条件，可一键开启 WSL2",
			"点击「🚀 一键开启」：授权 UAC 后自动完成 WSL 安装与虚拟机平台启用，随后按提示重启。"
	}
}

// evaluateForm 把三路安装形态信号归纳为一条体检项。
func evaluateForm(p ProbeResult) CheckItem {
	item := CheckItem{Key: "form", Label: "安装形态 (MSIX/MSI/启动器)"}
	var parts []string
	if p.WslMSIX != "" {
		parts = append(parts, "MSIX "+p.WslMSIX)
	}
	if p.WslMSI != "" {
		parts = append(parts, "MSI "+p.WslMSI)
	}
	if p.WslExeFile != "" {
		parts = append(parts, "启动器 wsl.exe")
	}
	if len(parts) == 0 {
		parts = append(parts, "无")
	}
	item.Value = strings.Join(parts, " · ")

	switch {
	case p.WslMSIX == "" && p.WslMSI == "" && p.WslExeFile == "":
		item.State = StateInfo
		item.Detail = "未检出任何安装形态"
	case p.WslMSIX == "" && p.WslMSI == "":
		item.State = StateInfo
		item.Detail = "仅存系统自带的 wsl.exe 启动器（OS 组件），未安装 WSL 主体"
	case p.WslMSIX != "" && p.WslMSI != "" && !strings.Contains(p.WslMSIX, p.WslMSI):
		item.State = StateWarn
		item.Detail = "MSIX 与 MSI 版本不一致（" + p.WslMSIX + " vs " + p.WslMSI + "），实际生效者以运行时版本为准"
	case p.WslMSIX != "" && p.WslMSI != "":
		item.State = StateOK
		item.Detail = "MSIX 用户包与 MSI 系统版共存——「设置」列表通常只显示其一，想彻底卸载须两处分别移除"
	default:
		item.State = StateOK
		item.Detail = "安装形态单一、注册表登记完整"
	}
	return item
}

func boolState(ok bool) string {
	if ok {
		return StateOK
	}
	return StateBad
}

func boolText(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func labelAll(items []CheckItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}
