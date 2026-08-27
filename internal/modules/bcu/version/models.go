// Package version 实现 Bulk Crap Uninstaller（BCU）版本管理引擎：
// GitHub Releases 远程列表、自包含便携 zip 下载（官方 sha256 四层校验）、
// 保布局解压隔离、本地整目录导入。
//
// BCU 的发布形态（与 ccswitch 的差异）：
//   - tag 只有两段（v6.2），真实版本号在资产名（6.2.0 / 6.1.0.1 三段或四段），
//     本包以资产名版本号为准、tag 只做一致性校验；
//   - bonus:自包含便携 zip 约 76MB（.NET 8 桌面运行时内置）真正免安装。
package version

// BCURelease 远程 GitHub Release 中可用的 BCU Windows 便携版。
type BCURelease struct {
	Version   string `json:"version"`   // 资产名解析的完整版本号，如 6.2.0 / 6.1.0.1（无 v）
	Tag       string `json:"tag"`       // release tag（如 v6.2）——下载 URL 拼路径用
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 BCUninstaller_6.2.0_portable.zip
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（GitHub API digest 去掉前缀）
}

// BCUVersionInfo 本地已安装的 BCU 版本信息。
type BCUVersionInfo struct {
	Version     string `json:"version"`     // 如 6.2.0
	ExePath     string `json:"exePath"`     // BCUninstaller.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录（exe 与 settings 数据同目录）
	Size        int64  `json:"size"`        // BCUninstaller.exe 大小（字节）
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
