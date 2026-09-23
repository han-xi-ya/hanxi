package version

// RammapRelease 远程版本。上游 RAMMap 为"同址覆盖式最新版"（无版本目录、无
// 历史资产），Version 取官方 zip 的 Last-Modified 日期（如 2026-03-26）作
// 唯一诚实版本语义——日期变即上游发新。列表恒单条。
type RammapRelease struct {
	Version   string `json:"version"`   // YYYY-MM-DD（Last-Modified 规一）
	Published string `json:"published"` // 原 Last-Modified HTTP 日期串
	IsPre     bool   `json:"isPre"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
}

// RammapVersionInfo 本地已装版本。Version 同为日期令牌；落位目录
// rammap_<YYYY-MM-DD>/，payload 平铺（RAMMap64.exe 直在版本目录，官方 zip 无
// 根目录包裹）。SHA256 为 exe 自算诊断哈希；上游无官方摘要，VerifiedHash 恒
// false（如实标注，走降级三层，见 manager.go 包注释）。
type RammapVersionInfo struct {
	Version      string `json:"version"`
	ExePath      string `json:"exePath"`
	Dir          string `json:"dir"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	InstalledAt  string `json:"installedAt"`
	IsImport     bool   `json:"isImport"`
	Source       string `json:"source"`
	VerifiedHash bool   `json:"verifiedHash"`
}

// DownloadProgress 下载进度事件载荷（事件键 rammap:version-download）。Stage
// 词表与家族一致：downloading/verify/extract/done/error。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
