// Package version 实现 RustDesk 版本管理引擎：GitHub Releases 远程列表、
// 便携 packer exe 下载（官方 sha256 digest 校验）、安装版 MSI 下载与
// msiexec 交互安装编排、系统已装版本注册表探测、隔离目录与本地导入。
//
// 与 ccswitch/everything 的最大差异：Windows 便携资产是**单个自解压 exe**
// （rust-portable packer，无 zip）——下载落盘即安装，无解压布局自检环节，
// 完整性以官方 digest + 字节数 + PE 魔数三重兜底。
//
// 安装版（MSI，perMachine）不走隔离目录托管：它是系统级形态（含 LocalSystem
// 服务，无人值守/锁屏被控依赖该服务），Hanxi 只负责"取包校验 → 发起上游
// 安装向导 → 探测识别与客户端拉起"，服务侧生命周期不归 Hanxi 管辖，
// 卸载引导至系统设置（详见 manager 的 Install 与 SystemInstall）。
package version

// 托管形态常量：区分隔离目录便携版（全托管）与系统安装版（客户端纳管、服务侧独立）。
const (
	FormPortable  = "portable"
	FormInstalled = "installed"
)

// RDRelease 远程 GitHub Release 中可用的 RustDesk Windows x64 版本。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验第一依据）。
type RDRelease struct {
	Version   string `json:"version"`   // 规范化展示版本，如 v1.4.9（上游 tag 无 v 前缀）
	Tag       string `json:"tag"`       // 上游原始 tag（1.4.9），下载 URL 构造用
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 便携 packer exe，如 rustdesk-1.4.9-x86_64.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）

	// 安装版 MSI 资产（rustdesk-1.4.9-x86_64.msi）。上游未附带或官方
	// digest 缺失时四字段留空——安装版路线对该版本不可用，便携主资产不受影响。
	InstallerName   string `json:"installerName"`
	InstallerURL    string `json:"installerUrl"`
	InstallerSize   int64  `json:"installerSize"`
	InstallerSHA256 string `json:"installerSha256"`
}

// RDVersionInfo 本地可用的 RustDesk 版本信息（隔离目录便携安装 + 系统安装版合流）。
type RDVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.3.0
	Form        string `json:"form"`        // portable / installed（见 Form* 常量）
	ExePath     string `json:"exePath"`     // 主程序完整路径（packer 外层 / 安装版 RustDesk.exe）
	Dir         string `json:"dir"`         // 版本隔离目录 / 系统安装目录
	Size        int64  `json:"size"`        // exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源路径（仅导入安装时有值）
}

// SystemInstall 系统级安装版（MSI perMachine）探测结果，注册表为唯一事实来源。
type SystemInstall struct {
	Version string `json:"version"` // 规范化 vX.Y.Z（注册表四段 DisplayVersion 截前三段，真机实证）
	ExePath string `json:"exePath"` // 安装目录下 RustDesk.exe（首字母大写，与便携定名不同）
	Dir     string `json:"dir"`     // 安装目录（InstallLocation，如 C:\Program Files\RustDesk\）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Kind    string `json:"kind"`    // "" = 便携 exe；installer = 安装版 MSI
	Stage   string `json:"stage"`   // resolve/downloading/verify/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
