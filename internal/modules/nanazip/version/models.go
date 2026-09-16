// Package version manages trusted NanaZip stable MSIXBundle resources.
package version

// Release 远端 stable MSIXBundle 发布条目。SHA256 取自 GitHub Release 正文声明；
// Stale 表示列表来自过期缓存（TTL 超时后的兜底旧数据）。
type Release struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Stale     bool   `json:"stale"`
}

// CachedPackage 本地已通过全链校验的可信包缓存条目（与 meta.json 一一对应）。
// Architectures 为 Bundle 内解出的目标架构列表，安装前据此匹配本机 arch。
type CachedPackage struct {
	Version          string   `json:"version"`
	Path             string   `json:"path"`
	Dir              string   `json:"dir"`
	Size             int64    `json:"size"`
	SHA256           string   `json:"sha256"`
	CachedAt         string   `json:"cachedAt"`
	VerificationMode string   `json:"verificationMode"`
	Architectures    []string `json:"architectures"`
}

// DownloadProgress 缓存下载阶段进度（Stage: downloading/verify/done），由 service 层转译为包操作进度事件。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
