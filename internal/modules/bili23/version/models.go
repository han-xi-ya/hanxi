// Package version 实现 Bili23 Downloader 版本管理引擎：GitHub Releases 远程列表、
// Windows 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
//
// 上游是 Python/PySide6 桌面应用，便携 zip 内为「静态 Python 运行时 + 源码 script/」
// 的完整目录（顶层单目录 Bili23-Downloader/，解压时剥离展平），体积 ~43MB（108MB 展开），
// 与 cc-switch 的"单 exe 即安装"不同：安装与导入单元都是整个目录。
package version

// Bili23Release 远程 GitHub Release 中可用的 Bili23 Downloader Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据）。
type Bili23Release struct {
	Version   string `json:"version"`   // 如 v2.15.0（tag 原样，存在 v2.00.7 前导零变体）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本（上游 -rc tag）
	AssetName string `json:"assetName"` // 如 Bili23-Downloader_2.15.0_windows_x64_portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// Bili23VersionInfo 本地已安装的 Bili23 Downloader 版本信息。
// 安装单元是整个目录（Bili23.exe + _pystand_static.int + script/ + runtime/ 等），
// 用户配置恒在 %APPDATA%\Bili23 Downloader\，与安装位置无关。
type Bili23VersionInfo struct {
	Version     string `json:"version"`     // 如 v2.15.0
	ExePath     string `json:"exePath"`     // Bili23.exe 完整路径（供启动/唤窗信使使用）
	Dir         string `json:"dir"`         // 版本隔离目录（整个安装单元）
	Size        int64  `json:"size"`        // 目录总大小（字节，展示用）
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
