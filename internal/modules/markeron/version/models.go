// Package version 实现 MarkerOn 版本管理引擎：远程 release 列表、端口 zip 下载、
// 保布局解压安装、隔离目录管理。
package version

// MarkerRelease 远程 GitHub Release 中可用的 MarkerOn Windows x64 便携版
type MarkerRelease struct {
	Version   string `json:"version"`   // 如 v2.9.4
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 MarkerOn_2.9.4_x64_portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址
	Size      int64  `json:"size"`      // 资产大小（字节）
}

// MarkerVersionInfo 本地已安装的 MarkerOn 版本信息
type MarkerVersionInfo struct {
	Version     string `json:"version"`     // 如 v2.9.4
	ExePath     string `json:"exePath"`     // MarkerOn.exe 完整路径（供启动/切换使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 markeron.portable 同目录）
	Size        int64  `json:"size"`        // MarkerOn.exe 大小（字节）
	SHA256      string `json:"sha256"`      // exe 自哈希（仅诊断展示，非校验依据）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
}

// DownloadProgress 下载过程实时进度
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/hash/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
