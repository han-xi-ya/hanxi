// Package version manages trusted NanaZip stable MSIXBundle resources.
package version

type Release struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Stale     bool   `json:"stale"`
}

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

type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
