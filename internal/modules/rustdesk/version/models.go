// Package version 实现 RustDesk 版本管理引擎：GitHub Releases 远程列表、
// 便携 packer exe 下载（官方 sha256 digest 校验）、隔离目录与本地导入。
//
// 与 ccswitch/everything 的最大差异：Windows 便携资产是**单个自解压 exe**
// （rust-portable packer，无 zip）——下载落盘即安装，无解压布局自检环节，
// 完整性以官方 digest + 字节数 + PE 魔数三重兜底。
package version

// RDRelease 远程 GitHub Release 中可用的 RustDesk Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验第一依据）。
type RDRelease struct {
	Version   string `json:"version"`   // 规范化展示版本，如 v1.4.9（上游 tag 无 v 前缀）
	Tag       string `json:"tag"`       // 上游原始 tag（1.4.9），下载 URL 构造用
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 rustdesk-1.4.9-x86_64.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// RDVersionInfo 本地已安装的 RustDesk 版本信息。
type RDVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.3.0
	ExePath     string `json:"exePath"`     // rustdesk.exe（packer 外层）完整路径
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源路径（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
