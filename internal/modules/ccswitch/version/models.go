// Package version 实现 CC Switch 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
package version

// CCRelease 远程 GitHub Release 中可用的 CC Switch Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 比 markeron 的纯字节数校验更强——cc-switch 是后发优势）。
type CCRelease struct {
	Version   string `json:"version"`   // 如 v3.20.0
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 CC-Switch-v3.20.0-Windows-Portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// CCVersionInfo 本地已安装的 CC Switch 版本信息。
type CCVersionInfo struct {
	Version     string `json:"version"`     // 如 v3.20.0
	ExePath     string `json:"exePath"`     // cc-switch.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 portable.ini 同目录）
	Size        int64  `json:"size"`        // cc-switch.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
