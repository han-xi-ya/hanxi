// Package version 实现 AirSnip 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载/校验/解包/落位主流程委托共享内核 packages/go/artifact，
// 隔离目录账本读扫与本地导入留在本包（领域知识）。
//
// 集成范围决策（机主 2026-11-03 拍板）：纯托管 + 内部命令通道——AirSnip 的核心
// 价值是热键驱动的全屏冻结覆盖层与贴图窗，属系统级形态，内嵌重做性价比为零，
// 截图/标注/长截图/贴图界面全部留在上游本体；Hanxi 侧只做版本管理、JobObject
// 启停，以及经由上游自己的托盘命令 ID 触发「截图 / 贴图 / 设置 / 退出」四个动作。
package version

import "hanxi/packages/go/hostfeed"

// AirsnipRelease 远程 GitHub Release 中可用的 AirSnip Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据，
// 与 ccswitch/litemonitor 同款——上游自 v0.3.0 起全量提供 digest，实测一致）。
type AirsnipRelease struct {
	Version   string `json:"version"`   // 如 v0.7.0
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 airsnip-v0.7.0-win-x64.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	// 注意上游 v0.7.0 起同 Release 并挂三份资产：手动下载用 zip、自动更新用
	// 裸 exe、清单 latest.json——findPortableAsset 只认 zip，裸 exe 与清单
	// 天然被 "-win-x64.zip" 后缀闸排除。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——上游只发一份 win-x64 便携归档（无安装器/无框架依赖变体），
	// 解包隔离目录直启单 exe。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// AirsnipVersionInfo 本地已安装的 AirSnip 版本信息。
type AirsnipVersionInfo struct {
	Version     string `json:"version"`     // 如 v0.7.0
	ExePath     string `json:"exePath"`     // airsnip.exe 完整路径（供启动/命令通道使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 所在处）
	Size        int64  `json:"size"`        // 版本目录整树字节和（度量失败时回退 exe 大小）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/extract/done/error（verify 由内核 Fetch 折进 download、place 折进 done，不造幻影步骤）
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
