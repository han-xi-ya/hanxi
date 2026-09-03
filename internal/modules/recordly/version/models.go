// Package version 实现 Recordly 版本管理引擎：GitHub Releases 远程列表（stable/beta 双通道）、
// NSIS 在线安装器下载（官方 digest sha256 + SHA256SUMS.txt 双源校验）、
// 静默安装进隔离目录、本地导入（整套安装目录 / 裸安装器）与卸载。
//
// 与 ccswitch（解 zip 保布局）的关键差异：上游 Windows 只有 NSIS 安装器
// （Recordly-windows-x64.exe），"免安装"经 `/S /D=<隔离目录>` 静默安装实现。
package version

// RecordlyRelease 远程 GitHub Release 中可用的 Recordly Windows x64 安装器。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验第一依据），
// 安装前还会尽力拉取官方 SHA256SUMS.txt 做交叉比对（第三只眼）。
type RecordlyRelease struct {
	Version   string `json:"version"`   // 如 v1.3.3 / v1.3.5-beta.2
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本（beta 通道条目）
	AssetName string `json:"assetName"` // 如 Recordly-windows-x64.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// RecordlyVersionInfo 本地已安装的 Recordly 版本信息。
type RecordlyVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.3.3
	ExePath     string `json:"exePath"`     // Recordly.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（NSIS 静默安装的落点）
	Size        int64  `json:"size"`        // Recordly.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
