// Package version 实现 LiteMonitor 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、嵌套布局解压安装、隔离目录管理与本地导入。
//
// 集成范围决策（用户拍板）：纯托管，不做网页监控内嵌——LiteMonitor 的核心价值
// 是桌面常驻横条/任务栏监控，显示界面留在上游本体；上游自带的网页版监控
// （WebServer，默认关闭）不纳入 Hanxi 视图，避免把上游配置纳入 Hanxi 直接改写。
package version

// LMRelease 远程 GitHub Release 中可用的 LiteMonitor Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 与 ccswitch 同款后发优势——上游全量提供 digest）。
type LMRelease struct {
	Version   string `json:"version"`   // 如 v1.3.6
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 LiteMonitor_v1.3.6-win-x64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// LMVersionInfo 本地已安装的 LiteMonitor 版本信息。
type LMVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.3.6
	ExePath     string `json:"exePath"`     // LiteMonitor.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 settings.json 同目录）
	Size        int64  `json:"size"`        // LiteMonitor.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
