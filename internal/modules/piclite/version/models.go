// Package version 实现 PicLite 版本管理引擎：GitHub Releases 远程列表、
// Windows x64 MSI 下载（官方 sha256 校验）、msiexec 管理提取安装、隔离目录管理与本地导入。
package version

// PicRelease 远程 GitHub Release 中可用的 PicLite Windows x64 MSI 安装包。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的第一依据）。
// 注意：上游不提供便携 zip，MSI 是唯一免管理员可拆解的资产形态
// （-setup.exe 为 perMachine NSIS，需要提权且写卸载注册表，不可用于托管）。
type PicRelease struct {
	Version   string `json:"version"`   // 如 v1.4.1
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 PicLite_1.4.1_x64_en-US.msi
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
}

// PicVersionInfo 本地已安装的 PicLite 版本信息。
// 有效载荷即单 exe（Tauri 静态链接，配置恒在 %APPDATA%\com.piclite.desktop，
// 与 exe 位置无关），目录内另有托管侧写入的 meta.json。
type PicVersionInfo struct {
	Version     string `json:"version"`     // 如 v1.4.1
	ExePath     string `json:"exePath"`     // piclite.exe 完整路径（供启动/唤窗使用）
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // piclite.exe 大小（字节）
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
