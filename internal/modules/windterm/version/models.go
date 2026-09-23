package version

// WindTermRelease 远程可用版本（GitHub releases 元数据投影）。
// Version 规范化为 vX.Y.Z 展示形态；AssetName/AssetURL 锁定 Windows x64 便携
// zip（上游唯一对本托管有效的资产形态）。
type WindTermRelease struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	IsPre     bool   `json:"isPre"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
}

// WindTermVersionInfo 本地已安装版本记录。ExePath 指向 payload 目录内的
// WindTerm.exe（zip 带 WindTerm_X.Y.Z 根目录包裹，托管保持原布局不解套）。
// SHA256 为 exe 自算诊断哈希（展示用）；VerifiedHash 如实标注是否经官方摘要
// 校验安装——上游 releases 全量无 GitHub digest（2026-09-23 API 实测 32 个
// release），远程安装恒为 false，走 vscode 降级三层先例（见 manager.go 包注释）。
type WindTermVersionInfo struct {
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

// DownloadProgress 下载进度事件载荷（事件键 windterm:version-download）。
// Stage 词表与全仓托管家族一致：downloading/verify/extract/done/error。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
