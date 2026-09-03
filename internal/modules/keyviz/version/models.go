// Package version 实现 Keyviz 版本管理引擎：GitHub Releases 远程列表、
// Windows MSI 下载（官方 sha256 校验）、msiexec 管理提取安装、隔离目录管理与本地导入。
//
// 上游 v2 正式版线只提供 .msi 安装包（唯一的 zip 便携资产停留在两年前的
// v2.0.0a3 预发布），与 piclite 同款处境，故复用 msiexec /a 管理提取路线。
package version

// KeyvizRelease 远程 GitHub Release 中可用的 Keyviz Windows MSI 安装包。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据）。
type KeyvizRelease struct {
	Version   string `json:"version"`   // 如 v2.1.1
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 keyviz_2.1.1_windows.msi
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// KeyvizVersionInfo 本地已安装的 Keyviz 版本信息。
type KeyvizVersionInfo struct {
	Version     string `json:"version"`     // 如 v2.1.1
	ExePath     string `json:"exePath"`     // keyviz.exe 完整路径（供托管启停使用）
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // keyviz.exe 大小（字节）
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
