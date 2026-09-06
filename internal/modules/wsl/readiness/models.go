// Package readiness 采集并评估本机 WSL2 就绪性：系统门槛（版本/架构/虚拟化）、
// 安装通道（GitHub API 是否被 403 拦截——「已禁止(403)」根因）、商店可用性与
// WSL 本体/发行版现状，汇总为带逐项目建议的体检报告。
package readiness

// ProbeResult 是只读 PowerShell 探针回传的本机环境快照（字段与探针 JSON 一一对应）。
type ProbeResult struct {
	OSBuild    int    `json:"osBuild"`    // Windows 内部版本号（19041+ 才支持 WSL2）
	OSUBR      int    `json:"osUBR"`      // 累积更新号
	ReleaseID  string `json:"releaseId"`  // 发行分支（如 25H2）
	Arch       string `json:"arch"`       // PROCESSOR_ARCHITECTURE
	Hypervisor bool   `json:"hypervisor"` // 虚拟机监控程序是否已在运行
	// VTFirmware 固件虚拟化开关。监控程序运行时 WMI 读不到真值（报 False 或空），
	// 属已知现象，评估时以 Hypervisor 优先。
	VTFirmware *bool `json:"vtFirmware"`
	VBS        *int  `json:"vbs"` // Win32_DeviceGuard 的 VBS 状态（2=运行中）
	Store      bool  `json:"store"`

	// WSL 安装形态三路信号（"卸了但没卸干净"取证）：MSIX 用户包 /
	// MSI 系统安装卸载注册表（含 ProductCode，正规卸载直接复用）/
	// System32 wsl.exe 文件版本。空串=该形态不存在。
	WslMSIX    string `json:"wslMsix"`
	WslMSI     string `json:"wslMsi"`
	WslMSICode string `json:"wslMsiCode"`
	WslExeFile string `json:"wslExeFile"`

	// 可选功能开关（Win32_OptionalFeature WMI，InstallState==1；DISM 真值对照校准）：
	// VirtualMachinePlatform 与旧版 Microsoft-Windows-Subsystem-Linux 各自独立。
	FeatureVM  bool `json:"vmPlatform"`
	FeatureWSL bool `json:"wslFeature"`
	// RebootPending 组件变更待重启官方台账（CBS RebootPending 键，重启后自动消失）。
	RebootPending bool `json:"rebootPending"`

	// APIGitHub / GitHub 为安装通道探测结果（Go 侧 netprobe 回填，非探针脚本产出）：
	// HTTP 状态码数字或 "net-fail" 等字符串。403 即 GitHub API 被拦截。
	APIGitHub any `json:"apiGitHub"`
	GitHub    any `json:"github"`
}

// Distro 单个已安装发行版的运行现状（wsl -l -v 表行）。
type Distro struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	State   string `json:"state"` // 原文透传（Running/正在运行、Stopped/已停止…）
	Version string `json:"version"`
}

// DistroOption 官方在线可安装发行版清单项（wsl --list --online）。
type DistroOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// 体检结论状态机。
const (
	StateOK          = "ok"
	StateWarn        = "warn"
	StateBad         = "bad"
	StateInfo        = "info"
	VerdictReady     = "ready"     // 全部硬门槛通过
	VerdictAttention = "attention" // 无阻塞但有注意项（如通道被拦）
	VerdictBlocked   = "blocked"   // 存在硬性阻塞（系统版本/虚拟化未开等）
)

// CheckItem 单条体检项。
type CheckItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"`  // ok | warn | bad | info
	Value  string `json:"value"`  // 机器值摘要（mono 展示）
	Detail string `json:"detail"` // 结论/修正建议
}

// Report 就绪性体检总报告（GetReadiness 载荷）。
type Report struct {
	Verdict       string      `json:"verdict"`
	VerdictTitle  string      `json:"verdictTitle"`
	VerdictDetail string      `json:"verdictDetail"`
	Checks        []CheckItem `json:"checks"`
	WslVersion    string      `json:"wslVersion"`
	Distros       []Distro    `json:"distros"`
	CollectedAt   string      `json:"collectedAt"`
	// VMPlatformEnabled 供前端状态驱动"开启/关闭虚拟机平台"按钮形态切换。
	VMPlatformEnabled bool `json:"vmPlatformEnabled"`
	// MachineArch 本机包架构口径（x64 / arm64，未知原样小写）：
	// 供 MSI 列表自动标记与排序"本机首选"，避免用户在 x64 机器上纠结 ARM 包。
	MachineArch string `json:"machineArch"`
	// RebootPending 系统真有组件变更欠着重启（引导条的唯一驱动源，操作语境不配说话）。
	RebootPending bool `json:"rebootPending"`
}
