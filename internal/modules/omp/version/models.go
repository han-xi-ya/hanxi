// Package version 实现「Oh My Pi (omp)」的版本管理与下载：GitHub Releases
// 远程列表、Windows 单文件 exe（omp-windows-x64.exe，Bun 自包含 + Rust 原生核）
// 下载与官方 sha256 校验，落盘到 versions/omp_<ver>/ 隔离目录并支持激活切换。
//
// 领域事实（2026-11-04 实测，均经 GitHub API 核验）：
//   - tag 恒为纯 semver vX.Y.Z（近 30 个 release 无一例外）；
//   - 每个 release 均携带 omp-windows-x64.exe（~244MB 自包含单文件，免运行时），
//     且资产 digest 字段（官方 sha256）全量可得——完整性第一层不缺位；
//   - 上游另有 omp-windows-arm64.exe 与 linux/darwin 家族、SHA256SUMS.txt 资产，
//     本模块只认 windows-x64；
//   - npm 包 @oh-my-pi/pi-coding-agent 需 bun 运行时，且裸名 oh-my-pi/omp 为
//     无关占名包——本模块不走 npm 安装面（详见 TROUBLESHOOTING #145）。
//
// 与其它托管模块的根本差异：**本模块不做进程托管**。omp 是纯 TUI 代理，必须
// 附着控制台终端才能交互，JobObject 静默启停/探测/唤窗都不成立；激活版本由
// 用户经 PATH 或 hanxi 嵌入终端使用。托盘候选同属负裁决（无窗可唤）。
// 版本树落位/校验/账本全部委托 packages/go/artifact 内核，与 ccswitch 同谱。
package version

// OmpRelease 上游 release 中可用的 Windows 单文件资产。
// SHA256 取自 GitHub API 资产 digest（官方计算，完整性校验第一依据）。
type OmpRelease struct {
	Version   string `json:"version"`   // 规范化展示版本，如 v18.16.0
	Tag       string `json:"tag"`       // 上游原始 tag（v18.16.0，与 Version 同形）
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // Windows 资产名，omp-windows-x64.exe
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// OmpVersionInfo 本地版本树中已落位的 omp 版本。
// Source 取 remote/imported；Active 为装配根 store 计算后的激活标记。
type OmpVersionInfo struct {
	Version     string `json:"version"`     // 如 v18.16.0（imported 兜底目录为 vimported-<hash12>）
	ExePath     string `json:"exePath"`     // 版本目录内 exe 完整路径
	Dir         string `json:"dir"`         // 版本隔离目录 versions/omp_<ver>
	Size        int64  `json:"size"`        // exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 落位时间 yyyy-MM-dd HH:mm:ss
	SHA256      string `json:"sha256"`      // 账本记录的实际 sha256（事后诊断）
	Source      string `json:"source"`      // remote|imported
	Active      bool   `json:"active"`      // 是否为当前激活版本
}

// DownloadProgress 下载过程实时进度（事件 omp:version-download 载荷）。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
