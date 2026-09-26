// Package version 实现 TranslucentTB 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
package version

import "hanxi/packages/go/hostfeed"

// TBRelease 远程 GitHub Release 中可用的 TranslucentTB Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验第一依据）。
// 版本号形如 2026.2（年份.序号，上游惯例，无 v 前缀）。
type TBRelease struct {
	Version   string `json:"version"`   // 如 2026.2
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 TranslucentTB-portable-x64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——TranslucentTB-portable-x64.zip 解压即用，机械判名与安装链
	// 事实一致（上游另有 msix/appx 包形态资产，托管不收）。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// TBVersionInfo 本地已安装的 TranslucentTB 版本信息。
type TBVersionInfo struct {
	Version     string `json:"version"`     // 如 2026.2
	ExePath     string `json:"exePath"`     // TranslucentTB.exe 完整路径（供启动/信使使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 settings.json 同目录）
	Size        int64  `json:"size"`        // TranslucentTB.exe 大小（字节）
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
