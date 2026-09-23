// Package sysinfo 借鉴 MooTool 的系统信息工具（N10）：本机软硬件静态档案一览。
//
// 借鉴纪律（docs/MOOTOOL_ANALYSIS.md：学机制不抄实现）：MooTool 走 Java OSHI
// （其底层即 WMI/WinAPI 聚合），本模块直采注册表 + WinAPI + 标准库，零新增
// 依赖、零 WMI 驻留进程；全部只读，不写任何系统状态。
//
// 划界（排期 N10 登记口径）：envcheck=开发工具链档案、litemonitor=托管第三方
// 实时监控、本模块=静态软硬件档案快照。个别事实（磁盘物理型号/序列号、显示器
// EDID 品牌型号）需 WMI 或卷句柄 DeviceIoControl，收益/复杂度不匹配，首版
// 如实缺席（前端不装作"没有=不存在"，字段级空值即事实边界）。
package sysinfo

// MachineInfo 整机与固件档案（注册表 HARDWARE\DESCRIPTION\System\BIOS 直读，
// 无需 WMI 的机型号路径）。
type MachineInfo struct {
	Hostname     string `json:"hostname"`
	Manufacturer string `json:"manufacturer"` // OEM（联想/戴尔/组装件为主板厂）
	Model        string `json:"model"`        // 整机型号 / 主板型号回落
	BIOSVendor   string `json:"biosVendor"`   // BIOS 厂商（LENOVO/American Megatrends…）
	BIOSVersion  string `json:"biosVersion"`  // BIOS 版本串
	ProductID    string `json:"productId"`    // Windows 产品 ID（注册表，本地展示用）
	SystemSKU    string `json:"systemSku"`
}

// OSInfo 操作系统档案。ProductName 经构建号规一（注册表在 Win11 上仍写
// "Windows 10 ..."的已知事实，规一逻辑带单测锁死）。
type OSInfo struct {
	ProductName   string `json:"productName"`
	Edition       string `json:"edition"`
	Version       string `json:"version"` // 22H2 / 23H2 形态
	Build         string `json:"build"`   // 22631.4317（BuildLabEx 优先，缺则 CurrentBuild）
	InstallDate   string `json:"installDate"`
	Is64Bit       bool   `json:"is64Bit"`
	Uptime        string `json:"uptime"`        // 人类可读（天/时/分）
	UptimeSeconds int64  `json:"uptimeSeconds"` // 机器可读（前端轮询/排序用）
}

// CPUInfo 处理器档案（HARDWARE 注册表 + 逻辑处理器枚举）。
type CPUInfo struct {
	Name     string `json:"name"`
	Vendor   string `json:"vendor"`
	SpeedMHz int    `json:"speedMHz"` // 标称标频（registry ~MHz 为当前值，如实注明口径）
	Cores    int    `json:"cores"`
	Logical  int    `json:"logical"`
	Sockets  int    `json:"sockets"`
}

// MemoryInfo 内存容量与当前水位（GlobalMemoryStatusEx；"静态档案"里的
// 唯一实时项——容量是静态的，占用是快照值，前端按"快照"语义呈现）。
type MemoryInfo struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	LoadPercent    uint32 `json:"loadPercent"`
	CommitTotal    uint64 `json:"commitTotal"`
	CommitLimit    uint64 `json:"commitLimit"`
}

// GPUInfo 显卡（display 设备类注册表逐项：驱动视角的硬件档案）。
type GPUInfo struct {
	Desc          string `json:"desc"`
	Provider      string `json:"provider"`
	DriverVersion string `json:"driverVersion"`
	DriverDate    string `json:"driverDate"`
}

// DisplayInfo 活动显示器（EnumDisplayDevices/Settings：分辨率与刷新率为
// 当前模式快照；EDID 品牌型号首版缺席，见包注释）。
type DisplayInfo struct {
	Name      string `json:"name"` // \\.\DISPLAY1 形态
	Width     int32  `json:"width"`
	Height    int32  `json:"height"`
	RefreshHz int32  `json:"refreshHz"`
	ColorBits int32  `json:"colorBits"`
	Primary   bool   `json:"primary"`
}

// VolumeInfo 逻辑卷档案（盘符/类型/容量/文件系统/卷标）。
type VolumeInfo struct {
	Letter     string `json:"letter"` // "C:"
	Label      string `json:"label"`
	FileSystem string `json:"fileSystem"`
	Type       string `json:"type"` // fixed / removable / network / cdrom
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

// NetInfo 网络接口档案（标准库 net：MAC/状态/IP；不含网关与 DNS——
// 网络诊断归 lan/publicip 模块，本表只给"这台机器有什么"）。
type NetInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MAC         string   `json:"mac"`
	MTU         int      `json:"mtu"`
	Up          bool     `json:"up"`
	Loopback    bool     `json:"loopback"`
	Addresses   []string `json:"addresses"`
}

// Report 一次全量档案快照（各段独立降级：单段采集失败其余照常送达）。
type Report struct {
	Machine  MachineInfo   `json:"machine"`
	OS       OSInfo        `json:"os"`
	CPU      CPUInfo       `json:"cpu"`
	Memory   MemoryInfo    `json:"memory"`
	GPUs     []GPUInfo     `json:"gpus"`
	Displays []DisplayInfo `json:"displays"`
	Volumes  []VolumeInfo  `json:"volumes"`
	Network  []NetInfo     `json:"network"`
	Errors   []string      `json:"errors,omitempty"` // 段级采集告警（空=全量成功；有告警如实透出前端）
}
