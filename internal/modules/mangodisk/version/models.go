// Package version 实现 MangoDisk 单文件便携版的远程发现、可信下载、隔离安装与完整性检查。
package version

// IntegrityState 本地安装的完整性状态。
type IntegrityState string

const (
	IntegrityVerified      IntegrityState = "verified"
	IntegrityLocalBaseline IntegrityState = "local-baseline"
	IntegrityDrifted       IntegrityState = "drifted"
	IntegrityInvalid       IntegrityState = "invalid"
)

// MangoDiskRelease 远程 GitHub Release 中可用的 Windows x64 portable GUI。
type MangoDiskRelease struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	IsPre     bool   `json:"isPre"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

// MangoDiskVersionInfo 本地已安装版本信息。
type MangoDiskVersionInfo struct {
	Version        string         `json:"version"`
	ExePath        string         `json:"exePath"`
	Dir            string         `json:"dir"`
	Size           int64          `json:"size"`
	InstalledAt    string         `json:"installedAt"`
	IsImport       bool           `json:"isImport"`
	Source         string         `json:"source"`
	Integrity      IntegrityState `json:"integrity"`
	IntegrityNote  string         `json:"integrityNote"`
	ExpectedSHA256 string         `json:"expectedSha256"`
	CurrentSHA256  string         `json:"currentSha256"`
	FileVersion    string         `json:"fileVersion"`
	ProductName    string         `json:"productName"`
}

// DownloadProgress 下载与安装过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // resolve/downloading/verify/install/done/error
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
