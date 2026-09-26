// Package version 实现 Everything 版本管理引擎：官网下载页槽位解析（remote.go，
// 非标准 releases 形状的资产 URL/digest 解析层留本模块），"下载 → 校验 → 解包 →
// 落位"主流程委托共享内核 packages/go/artifact，另含本地整套导入链（离线通道）。
package version

import "hanxi/packages/go/hostfeed"

// EverythingRelease 远程可用版本槽位（官网仅暴露各通道当前最新版，
// 详见 https://www.voidtools.com/downloads/ 的「稳定版 / 1.5 Beta」区块）。
type EverythingRelease struct {
	Version   string `json:"version"`   // 如 1.5.0.1422b（无 v 前缀，与官方资产名一致）
	Channel   string `json:"channel"`   // stable / beta（来源于下载页区块标题）
	Published string `json:"published"` // 资产 Last-Modified（yyyy-MM-dd，抓取失败则保留原样）
	AssetURL  string `json:"assetUrl"`  // x64 便携 zip 直链
	SHA256    string `json:"sha256"`    // 官方 sha256 清单中该 zip 的哈希（清单不可得时为空；下载链视为无校验安装一律拒装）
	Size      int64  `json:"size"`      // HEAD Content-Length（探测失败为 0，跳过字节级校验）
	Stale     bool   `json:"stale"`     // 来自旧缓存或内置快照，非实时数据
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——官网下载页 x64 资产即按钮原文 "Download Portable ZIP" 的
	// 便携 zip（解压直用，notesOf 形态词表已按此实证）；托管安装链
	// （下载 → 解包 → 平铺布局自检）自证同一事实。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// EverythingVersionInfo 本地已安装的 Everything 版本信息
type EverythingVersionInfo struct {
	Version     string `json:"version"`     // 如 1.5.0.1422b
	ExePath     string `json:"exePath"`     // Everything.exe 完整路径（供启动/信使使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与配置/索引库同目录）
	Size        int64  `json:"size"`        // exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否为本地导入（带用户配置与索引库）
	Source      string `json:"source"`      // 来源（官方下载资产名 / 导入源目录）
}

// DownloadProgress 下载过程实时进度
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
