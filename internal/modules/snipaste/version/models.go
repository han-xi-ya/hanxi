// Package version 实现 Snipaste 官网免安装版的版本发现、完整性校验与隔离安装。
package version

// SnipasteRelease 表示官网可下载的 Windows x64 免安装版本。
type SnipasteRelease struct {
	Version       string `json:"version"`
	Published     string `json:"published"`
	IsPre         bool   `json:"isPre"`
	AssetName     string `json:"assetName"`
	AssetURL      string `json:"assetUrl"`
	Size          int64  `json:"size"`
	OfficialHash  string `json:"officialHash"`
	HashAlgorithm string `json:"hashAlgorithm"`
	Stale         bool   `json:"stale"`
}

// SnipasteVersionInfo 表示一个本地隔离安装版本。
type SnipasteVersionInfo struct {
	Version          string `json:"version"`
	ExePath          string `json:"exePath"`
	Dir              string `json:"dir"`
	Size             int64  `json:"size"`
	InstalledAt      string `json:"installedAt"`
	IsImport         bool   `json:"isImport"`
	Source           string `json:"source"`
	PackageSHA256    string `json:"packageSha256"`
	VerificationMode string `json:"verificationMode"`
}

// DownloadProgress 表示下载、校验与安装阶段的实时进度。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
