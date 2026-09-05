// Package version 实现 Rufus 版本管理引擎：GitHub Releases 远程列表、
// 单文件便携 exe 下载（GitHub API digest 官方 sha256 三重校验）、
// 隔离目录管理与本地导入。
//
// 集成范围决策（用户拍板）：纯托管，不做启动盘制作功能内嵌——
// 磁盘级写入（分区/格式化/镜像落盘）是工作站上数据销毁风险最高的操作，
// 上游 Rufus 的 GUI（含二次确认、坏块检查、DBX 策略、日志窗等完整交互链）
// 本身就是产品全形态，在 Hanxi 重做既无性价比又引入新风险。
//
// 上游资产形态（GitHub API 实证，v4.9~v4.15 连续多版稳定）：
// 单文件 exe 无 zip——"安装版" rufus-X.Y.exe 与便携版 rufus-X.Yp.exe
// 字节级同哈希（同一二进制），托管取 p.exe 形态语义明确。
package version

// RufusRelease 远程 GitHub Release 中可用的 Rufus Windows x64 便携单文件。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 与 ccswitch/litemonitor 同款后发优势——上游全量提供 digest）。
type RufusRelease struct {
	Version   string `json:"version"`   // 如 v4.15（tag 原样带 v 前缀）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 rufus-4.15p.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// RufusVersionInfo 本地已安装的 Rufus 版本信息。
type RufusVersionInfo struct {
	Version     string `json:"version"`     // 如 v4.15
	ExePath     string `json:"exePath"`     // rufus.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 rufus.ini 同目录）
	Size        int64  `json:"size"`        // rufus.exe 大小（字节）
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
