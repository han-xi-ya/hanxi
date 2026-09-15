// Package version 实现「抖音下载器」(Douzy) 的版本管理与下载：GitHub Releases
// 远程列表、Windows 安装包（Douzy-Setup-*.exe，NSIS）下载与官方 sha256 校验，
// 落盘到 versions/douzy_<ver>/ 隔离目录供用户自行安装。
//
// 与 ccswitch/rustdesk 托管模块的根本差异：本模块**不做进程托管**——只负责
// "取安装包 → 校验 → 交还用户安装"，不建隔离运行、不探测启停、不接管窗口。
// 原因：桌面版 Douzy 尚处上游内测期、Electron 壳源码未公开、Windows 仅有
// NSIS 安装版无便携 zip，三者在"托管"上都是硬伤；停在"版本+下载"边界最稳妥。
package version

// DouzyRelease 上游 desktop-v 系列 release 中可用的 Windows 安装包。
// SHA256 取自 GitHub API 资产 digest（官方计算，完整性校验第一依据）。
type DouzyRelease struct {
	Version   string `json:"version"`   // 规范化展示版本，如 v0.11.5
	Tag       string `json:"tag"`       // 上游原始 tag desktop-v0.11.5（下载 URL 构造用）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // Windows 安装包，如 Douzy-Setup-0.11.5.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// DouzyVersionInfo 本地已下载的 Douzy 安装包信息（隔离目录）。
// 字段名沿用托管模块约定（exePath/installedAt）以便复用前端 composable，
// 但语义是"已下载的 Setup 安装包"而非"已安装的可执行程序"。
type DouzyVersionInfo struct {
	Version     string `json:"version"`     // 如 v0.11.5
	ExePath     string `json:"exePath"`     // 安装包完整路径（Douzy-Setup-*.exe）
	Dir         string `json:"dir"`         // 版本隔离目录 versions/douzy_0.11.5
	Size        int64  `json:"size"`        // 安装包大小（字节）
	InstalledAt string `json:"installedAt"` // 下载完成时间 yyyy-MM-dd HH:mm:ss
	SHA256      string `json:"sha256"`      // 已校验的实际 sha256（事后诊断）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/verify/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
