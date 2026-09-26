// Package version 实现 GoNavi 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
//
// 完整性策略（四层）：①GitHub API 资产 digest 官方摘要（备用 SHA256SUMS
// 附件，两路皆缺的 release 不入列表、拒无校验安装）②声明字节数与流式上限
// 双核（内核 artifact.Fetch）③ZipSlip/炸弹/CRC32 全量解包闸门（内核
// artifact.UnpackZip）④落位布局自检（GoNavi.exe 在解压根存在非空）。
//
// 上游纪律（Syngnat/GoNavi，Wails v2 单 exe 应用，实证 2026-09）：稳定版
// tag 恒 vX.Y.Z；另有 dev-latest 滚动预发布通道，其资产与稳定版同名同形，
// 必须在解析层按 prerelease 过滤，绝不入托管列表。禁用上游 latest.json 内
// 的私有 VPS 镜像 URL——下载一律 GitHub 直链 + 家族镜像回退通道。
// 许可 Apache-2.0：解包保布局使 LICENSE/NOTICE 天然随版本目录保留。
package version

import "hanxi/packages/go/hostfeed"

// GoNaviRelease 远程 GitHub Release 中可用的 GoNavi Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据）；
// digest 缺失但存在官方 SHA256SUMS 附件时改走备用摘要源（SumsAsset 携带
// 附件名，下载时解析比对，仍属官方信任根）。v0.8.9 起才有 Windows 便携
// 资产，更早版本天然不入列表（列表可为空，非错误）。
type GoNaviRelease struct {
	Version   string `json:"version"`   // 如 v0.8.9
	Published string `json:"published"` // 发布时间（RFC3339）
	AssetName string `json:"assetName"` // 如 GoNavi-0.8.9-Windows-Amd64-Portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀；备用源时为空）
	// SumsAsset 备用摘要源：官方 SHA256SUMS 附件名（仅 digest 缺失时非空，
	// 下载时经镜像拉取解析出 zip 的期望摘要）。
	SumsAsset string `json:"sumsAsset,omitempty"`
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，纯展示，下载路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——GoNavi-x-y-z-Windows-Amd64-Portable.zip 解压即用（上游另有
	// Installer.msi / Portable.exe SFX 形态资产，托管不收）。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// GoNaviVersionInfo 本地已安装的 GoNavi 版本信息。
// SHA256 为账本内 exe 落位摘要（Download/ImportLocal 成功时实测记入）；
// VerifiedHash 徽章语义对齐托管族（verifiedHash）：远程下载链经官方摘要
// 校验为 true，本地导入无官方参照为 false。HashDrifted 为账外漂移护栏：
// ListInstalled 刷新时对"落位后 mtime 变新"的 exe 复算摘要，与账本不符
// 即如实标记——只报告不处置，不撒谎（成本控制见 driftCheck 注释）。
type GoNaviVersionInfo struct {
	Version      string `json:"version"`      // 如 v0.8.9
	ExePath      string `json:"exePath"`      // GoNavi.exe 完整路径（供启动使用）
	Dir          string `json:"dir"`          // 版本隔离目录（exe 与 LICENSE/NOTICE 同目录）
	Size         int64  `json:"size"`         // 版本目录整树字节和（度量失败回退 exe 大小）
	InstalledAt  string `json:"installedAt"`  // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport     bool   `json:"isImport"`     // 是否经「导入本地」安装（非远程下载）
	Source       string `json:"source"`       // 导入来源目录（仅导入安装时有值）
	SHA256       string `json:"sha256"`       // 账本内 exe 落位摘要（可为空=账本无摘要）
	VerifiedHash bool   `json:"verifiedHash"` // 安装时是否经官方摘要校验
	HashDrifted  bool   `json:"hashDrifted"`  // 落位后 exe 是否发生账外漂移
	DriftNote    string `json:"driftNote"`    // 漂移复查明细（如实转述，含无法比对场景）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
