// Package version 实现 LiteMonitor 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载/校验/解包/落位主流程委托共享内核 packages/go/artifact，
// 隔离目录账本读扫与本地整套导入留在本包（领域知识）。
//
// 集成范围决策（用户拍板）：纯托管，不做网页监控内嵌——LiteMonitor 的核心价值
// 是桌面常驻横条/任务栏监控，显示界面留在上游本体；上游自带的网页版监控
// （WebServer，默认关闭）不纳入 Hanxi 视图，避免把上游配置纳入 Hanxi 直接改写。
package version

import "hanxi/packages/go/hostfeed"

// LMRelease 远程 GitHub Release 中可用的 LiteMonitor Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 与 ccswitch 同款后发优势——上游全量提供 digest）。
type LMRelease struct {
	Version   string `json:"version"`   // 如 v1.3.6
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 LiteMonitor_v1.3.6-win-x64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——remote.go findPortableAsset 只收上游 win 线唯一的
	// LiteMonitor_<ver>-win-x64.zip 便携归档（无安装器变体），解包隔离目录
	// 直启；资产名无 portable 字样，机械判名 Classify 保守降为 archive，
	// 形态以托管安装链事实自证为准。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// LMVersionInfo 本地已安装的 LiteMonitor 版本信息。
type LMVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.3.6
	ExePath     string `json:"exePath"`     // LiteMonitor.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 settings.json 同目录）
	Size        int64  `json:"size"`        // LiteMonitor.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/extract/done/error（verify 由内核 Fetch 折进 download、install 折进 done，不造幻影步骤）
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
