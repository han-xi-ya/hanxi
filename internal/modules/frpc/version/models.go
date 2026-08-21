// Package version 实现 frp 版本管理引擎：远程列表、下载硬校验、解压隔离、本地导入。
package version

// FrpRelease 远程 GitHub Release 中可用的 frp Windows amd64 版本
type FrpRelease struct {
	Version   string `json:"version"`   // 如 v0.61.1
	Published string `json:"published"` // 发布时间（RFC3339）
	IsPre     bool   `json:"isPre"`     // 是否为预发布版本
	AssetName string `json:"assetName"` // 如 frp_0.61.1_windows_amd64.zip
	AssetURL  string `json:"assetUrl"`  // 直链下载地址
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256-checksums.txt 中对应的哈希
}

// FrpVersionInfo 本地已安装的 frp 版本信息
type FrpVersionInfo struct {
	Version     string `json:"version"`     // 如 v0.61.1
	ExePath     string `json:"exePath"`     // frpc.exe 完整路径（供实例启动使用）
	Size        int64  `json:"size"`        // frpc.exe 大小（字节）
	SHA256      string `json:"sha256"`      // 文件哈希（校验依据）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否手动导入（非官方下载）
}

// DownloadProgress 下载过程实时进度
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/hash/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
