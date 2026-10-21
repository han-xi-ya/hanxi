// Package version 实现 HEU KMS Activator 的版本引擎：GitHub Releases 远程列表、
// RAR5 下载/校验/解包/落位（下载与落位主流程委托共享内核 packages/go/artifact）、
// 以及本地导入通道。
//
// 上游发布形态实证（2026-10-16 首侦、2026-10-19 更正，逐条取证见
// docs/TROUBLESHOOTING.md #108），三个代际：
//   - zip（24.1.0~40.0.0，2021-07~2023-10，24 版）；
//   - rar（26.2.0~61.0.0，19 版）；
//   - iso+rar（61.1.0~最新 64.0.0，11 版）。
//
// 走哪条通道、为什么：
//   - **iso 不碰**：ISO9660 侧只有 3 条记录（`.`/`..`/135 字节 README.TXT，其正文
//     自述 "This disc contains a UDF file system…"），真实文件全在 UDF 侧——
//     go-diskfs v1.9.4 无 udf 实现，系统自带 bsdtar 3.6.2 同样读不到 UDF，
//     机主亦明确不走挂载/解析镜像这条路；
//   - **rar 可解**：`github.com/nwaples/rardecode`（BSD-2-Clause、纯 Go、零传递
//     依赖）支持 RAR 5.0，实测解 64.0.0 的 rar 得主程序 SHA256
//     `6ad72af4…813e8a5c`，与上游在包内更新日志里逐版公布的 exe 哈希**逐字节
//     一致**；该包**未加密**（更新日志里的"解压密码 HEU168"是作者信息栏文字，
//     不构成包加密事实）；解包层自带 CRC 校验（实测篡改一包内字节报
//     `rardecode: bad file checksum`）。
//
// 自动安装的边界（内核纪律的如实体现）：`artifact.Fetch` 强制官方 SHA256
// （"托管下载不允许无校验安装"），而上游只有 63.1.0 起的 9 个 release 挂了
// GitHub `digest`。因此远程表**只列可自动安装的版本**（有 rar 且有官方摘要），
// 更早版本不给前端造"看得见点不动"的形态——需要旧版走「导入本地」。
package version

import "hanxi/packages/go/hostfeed"

// HeuRelease 远程 GitHub Release 中**可自动安装**的一条 HEU 版本记录。
//
// 入选即代表：tag 为纯三段数字、非 draft、存在可解包的 rar 资产、且上游/
// GitHub 已为该资产发布官方 sha256 摘要（见本包注释"自动安装的边界"）。
type HeuRelease struct {
	Version   string `json:"version"`   // 如 64.0.0（上游 tag 原样，无 v 前缀）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	// PageURL 该版发布页（iso 手取路径的备援入口，前端"发布页"直达）。
	PageURL string `json:"pageUrl"`
	// AssetName 托管资产名（恒为 rar——唯一具备纯 Go 解包通道的形态）。
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"` // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`     // 资产大小（字节）
	SHA256    string `json:"sha256"`   // 官方 sha256（digest 去前缀；入选前置条件，恒非空）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态。上游 rar 资产机械判型落 archive（裸压缩归档、无
	// portable 字样），但安装链是"解包取单 exe 直启"，语义即便携。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（remote.go 逐行回填 Release.Form）。
const hostedForm = hostfeed.FormPortable

// HeuVersionInfo 本地已安装的 HEU 版本信息。
//
// Version 一律用**上游 tag 原形**（64.0.0，无 v 前缀）：本工具 tag 与展示串都不带
// v，硬加前缀会让前端回传值与 resolveVersionDir 的入参形状多一层转换，不值得。
type HeuVersionInfo struct {
	Version     string `json:"version"`     // 如 64.0.0（或兜底目录的 imported-时间戳）
	ExePath     string `json:"exePath"`     // 主 exe 完整路径（启动/唤窗用）
	ExeName     string `json:"exeName"`     // 主 exe 文件名（上游名随版本漂移，单独给）
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // 主 exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 账本来源（remote / imported）
	// SHA256 落位时对主 exe 计算的摘要（账本诊断用；未签名工具常被安全软件改动，
	// 与当前实况对照即可分辨"文件被动过"与"版本装错了"两类问题）。
	SHA256 string `json:"sha256"`
}

// DownloadProgress 下载→解包→落位全程的实时进度（事件 heukms:version-download）。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/extract/done/error（verify 由内核 Fetch 折进 download、install 折进 done，不造幻影步骤）
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
