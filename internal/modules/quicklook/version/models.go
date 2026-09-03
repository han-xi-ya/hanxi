// Package version 实现 QuickLook 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
//
// 上游 QuickLook 每个正式版并列发布 .7z/.appx/.exe/.msi/.zip 五种资产，其中
// 唯有 .zip 是免安装便携包（根目录即 QuickLook.exe + portable.lock + 原生/插件
// DLL 整套，实测 v4.5.0 布局）。故本包锁定 .zip 资产：.exe/.msi/.appx 是写系统
// 的安装器（违背托管"解压即跑、随汉溪干净退出"前提），.7z 标准库无法原生解压。
package version

// QuickLookRelease 远程 GitHub Release 中可用的 QuickLook Windows 便携 zip。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 与 ccswitch 同款——GitHub 自 2024 起对新资产全覆盖 digest）。
type QuickLookRelease struct {
	Version   string `json:"version"`   // 如 4.5.0（上游 tag 无 v 前缀）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 QuickLook-4.5.0.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// QuickLookVersionInfo 本地已安装的 QuickLook 版本信息。
type QuickLookVersionInfo struct {
	Version     string `json:"version"`     // 如 4.5.0
	ExePath     string `json:"exePath"`     // QuickLook.exe 完整路径（供托管启停使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 portable.lock、插件同处）
	Size        int64  `json:"size"`        // QuickLook.exe 大小（字节）
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
