// Package version 实现 Paseo 版本管理引擎：GitHub Releases 远程列表（stable/beta 双通道）、
// 官方 Windows 便携 zip（electron-builder win zip target，资产名 Paseo-Setup-<ver>-<arch>.zip）
// 下载与四层完整性校验、保布局解压进多版本隔离目录、本地导入与卸载。
//
// 与 recordly（NSIS 单目录）的关键差异：上游同时发布 win zip 便携形态
// （electron-builder.yml win.target = [nsis, zip]，v0.7.0 起连续在发），
// 解压即运行、无注册表卸载语义，因此走 vscode 便携同构的多版本隔离目录
// versions/paseo_X.Y.Z/，"切换版本"零重装。
//
// 数据归属（集成决策）：Paseo 无 data\ 便携激活器，Electron 数据恒在
// %APPDATA%\Paseo、daemon 数据恒在 ~/.paseo——托管实例与用户自装实例
// 共享同一份用户目录数据（拍板方案，理由见模块 module.go 包注释），
// 故解压/导入均不做任何数据目录搬运或隔离注入。
package version

// PaseoRelease 远程 GitHub Release 中可用的 Paseo Windows 便携 zip。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验第一依据，
// 缺 digest 的 release 一律不入列表——与 recordly 同纪律）。
type PaseoRelease struct {
	Version   string `json:"version"`   // 如 0.8.0 / 0.8.0-beta.1（tag 去掉 v 前缀，与版本目录名同形）
	Tag       string `json:"tag"`       // GitHub tag 原文（v0.8.0，下载 URL 路径用）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本（beta 通道条目）
	AssetName string `json:"assetName"` // 如 Paseo-Setup-0.8.0-x64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// PaseoVersionInfo 本地已安装的 Paseo 版本信息（一个版本目录一条）。
type PaseoVersionInfo struct {
	Version      string `json:"version"`      // 如 0.8.0 / 0.8.0-beta.1（版本目录名所载，无 v 前缀）
	ExePath      string `json:"exePath"`      // Paseo.exe 完整路径（供启动/唤窗使用）
	Dir          string `json:"dir"`          // 版本隔离目录（解压落点）
	Size         int64  `json:"size"`         // Paseo.exe 大小（字节）
	InstalledAt  string `json:"installedAt"`  // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport     bool   `json:"isImport"`     // 是否经「导入本地」安装（非远程下载）
	Source       string `json:"source"`       // 导入来源目录 / 下载资产名
	VerifiedHash bool   `json:"verifiedHash"` // 是否经官方 sha256 验证（导入安装为 false）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
