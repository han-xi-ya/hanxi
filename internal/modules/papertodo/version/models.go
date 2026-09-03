// Package version 实现 PaperTodo 版本管理引擎：GitHub Releases 双变体远程列表、
// 绿色单 exe 下载与多层完整性校验、固定单目录覆盖安装、本地导入与卸载（保留便签数据）。
package version

// 运行库变体常量（与上游资产后缀同名，持久化在 papertodo.json，取值即协议）。
const (
	VariantSelfContained = "self-contained" // 内嵌 .NET 10 运行时，体积 ~71MB，零依赖
	VariantNoRuntime     = "no-runtime"     // 框架依赖版，体积 ~2.4MB，需系统 .NET 10 桌面运行时
)

// RequiresDesktopMajor no-runtime 变体依赖的 WindowsDesktop.App 主版本
// （上游 README 明确 .NET 10；.NET 运行时不跨大版本回退）。
const RequiresDesktopMajor = "10"

// ValidVariant 校验变体取值（service 层入口防御）。
func ValidVariant(v string) bool {
	return v == VariantSelfContained || v == VariantNoRuntime
}

// PaperAsset 单个发布资产的下载要素。
// SHA256 为 GitHub API digest（官方计算）。实证：PaperTodo 当前所有 release
// 资产均无 digest 字段——存在时（GitHub 逐批回刷后）Download 会升级为官方哈希硬校验，
// 缺失时降级为"字节数 + PE 版本核对 + sha256 指纹存档"链（详见 manager 注释）。
type PaperAsset struct {
	Name   string `json:"name"`   // 资产文件名（原样用于拼接上游下载 URL）
	Size   int64  `json:"size"`   // API 声明字节数
	SHA256 string `json:"sha256"` // 官方 sha256（可为空 = 上游未提供 digest）
}

// PaperRelease 远程可用的 PaperTodo Windows x64 GA 版本（双变体齐备才入列表）。
type PaperRelease struct {
	Version       string     `json:"version"`       // tag 原样，如 v3.31
	Published     string     `json:"published"`     // 发布时间（RFC3339）
	SelfContained PaperAsset `json:"selfContained"` // 完整版资产
	NoRuntime     PaperAsset `json:"noRuntime"`     // 精简版资产
}

// PaperVersionInfo 本地托管安装信息（固定单目录 versions/papertodo，至多一条）。
type PaperVersionInfo struct {
	Version     string `json:"version"`     // 权威版本（hanxi-meta tag → PE FileVersion → vunknown 兜底）
	Variant     string `json:"variant"`     // self-contained / no-runtime / ""（导入安装无记录）
	ExePath     string `json:"exePath"`     // PaperTodo.exe 完整路径
	Dir         string `json:"dir"`         // 托管目录（exe 与 data.json 同目录）
	Size        int64  `json:"size"`        // exe 字节数
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录 / 下载资产文件名
	HasData     bool   `json:"hasData"`     // 同目录是否存在 data.json 便签数据
	AssetSHA256 string `json:"assetSha256"` // 下载时计算的 sha256 指纹（审计/篡改感知）
	OfficialSHA string `json:"officialSha"` // 官方 digest（上游提供时非空，已硬校验）
	PEVersion   string `json:"peVersion"`   // PE FileVersion 资源（"" = 读取失败，仅核对降级）
}

// DownloadProgress 下载过程实时进度（事件 papertodo:version-download 载荷）。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/done/error（单 exe 无解压阶段）
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
