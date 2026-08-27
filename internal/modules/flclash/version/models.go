// Package version 实现 FlClash 版本管理引擎：
// GitHub Releases 远程列表、Windows 便携 zip 下载（官方 sha256 四层校验）、
// 保布局解压隔离、本地整目录导入。
//
// FlClash 的发布形态（侦查实证）：
//   - Windows 便携资产 FlClash-<ver>-windows-amd64.zip（Flutter 自包含，
//     FlClash.exe + flutter_windows.dll + data/，解压即用，约 58MB）；
//   - tag 与资产版本完全同形（v0.8.96 == 0.8.96），digest 全量覆盖，
//     且另有 SHA256SUMS 资产双保险（digest 已是权威层，未重复使用）；
//   - 数据目录在 %APPDATA%（非 exe 目录），单实例为文件锁第二实例直接退出。
package version

// FlClashRelease 远程 GitHub Release 中可用的 FlClash Windows x64 便携版。
type FlClashRelease struct {
	Version   string `json:"version"`   // 如 0.8.96（无 v）
	Tag       string `json:"tag"`       // release tag（如 v0.8.96）——下载 URL 拼路径用
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 FlClash-0.8.96-windows-amd64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（GitHub API digest 去掉前缀）
}

// FlClashVersionInfo 本地已安装的 FlClash 版本信息。
type FlClashVersionInfo struct {
	Version     string `json:"version"`     // 如 0.8.96
	ExePath     string `json:"exePath"`     // FlClash.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // FlClash.exe 大小（字节）
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
