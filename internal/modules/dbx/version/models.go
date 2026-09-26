// Package version 实现 DBX 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验）、保布局解压安装、隔离目录管理与本地导入。
//
// 完整性策略（四层）：①GitHub API 资产 digest 官方摘要（唯一信任根——上游
// 同名 .sig 为 minisign Ed25519 签名，本托管不实现 minisign 校验，如实以
// GitHub digest 为准；digest 缺失的 release 不入列表、拒无校验安装，宁拒不
// 猜）②声明字节数与流式上限双核（内核 artifact.Fetch）③ZipSlip/炸弹/CRC32
// 全量解包闸门（内核 artifact.UnpackZip）④落位布局自检（DBX.exe 与
// portable.dbx 同在解压根；zip 内 portable-update.json 的 executable_sha256
// 另作 exe 级旁证复核）。
//
// 上游纪律（t8y2/dbx，Tauri v2 数据库客户端，实证 2026-09）：稳定版 tag 恒
// vX.Y.Z，日更节奏（列表按版本降序正常展示，不加强轮询）；Windows x64 便携
// 资产恒 DBX_<版本>_x64-portable.zip，setup/msi/offline/win7/arm64 变体全
// 排除。zip 根平铺五条目：DBX.exe + LICENSE + README.md + portable.dbx +
// portable-update.json——portable.dbx 为空标记文件，应用据其存在走便携数据
// 模式，任何落位/导入链必须保留勿删。许可 Apache-2.0：解包保布局使许可
// 文本天然随版本目录保留。
package version

import "hanxi/packages/go/hostfeed"

// DBXRelease 远程 GitHub Release 中可用的 DBX Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的唯一依据）；
// digest 缺失即不入列表（上游 .sig 属 minisign 通道，本托管不消费，没有
// 第二官方摘要源）。
type DBXRelease struct {
	Version   string `json:"version"`   // 如 v1.5.3
	Published string `json:"published"` // 发布时间（RFC3339）
	AssetName string `json:"assetName"` // 如 DBX_1.5.3_x64-portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，纯展示，下载路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——DBX_x-y-z_x64-portable.zip 解压即用（上游另有 setup.exe /
	// msi / offline 安装形态资产与 minisign .sig 附属，托管不收）。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// DBXVersionInfo 本地已安装的 DBX 版本信息。
// SHA256 为账本内 exe 落位摘要（Download/ImportLocal 成功时实测记入）；
// VerifiedHash 徽章语义对齐托管族（verifiedHash）：远程下载链经官方摘要
// 校验为 true，本地导入无官方参照为 false。HashDrifted 为账外漂移护栏：
// ListInstalled 刷新时对"落位后 mtime 变新"的 exe 复算摘要，与账本不符
// 即如实标记——只报告不处置，不撒谎（成本控制见 driftCheck 注释）。
// DBX 是上游自带更新器的 Tauri 应用：漂移徽章正是为"应用自更新原地换
// exe"这一必然发生的场景而设。
type DBXVersionInfo struct {
	Version      string `json:"version"`      // 如 v1.5.3
	ExePath      string `json:"exePath"`      // DBX.exe 完整路径（供启动使用）
	Dir          string `json:"dir"`          // 版本隔离目录（exe 与便携标记/许可文本同目录）
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
