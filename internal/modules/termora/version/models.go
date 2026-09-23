package version

// TermoraRelease 远程可用版本（GitHub releases 元数据投影）。
// Version 规范化为 vX.Y.Z[-beta.N] 展示形态；上游 2.x 线全部挂 prerelease
// 标记（beta 即事实主干），IsPre 如实透出由面板"预发布"徽标呈现。
type TermoraRelease struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	IsPre     bool   `json:"isPre"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"`
	Size      int64  `json:"size"`
}

// TermoraVersionInfo 本地已安装版本记录。ExePath 指向 payload 目录内的
// Termora.exe（jpackage app-image 固定布局：包根 Termora/ 包裹，主 exe +
// app/（jar 与 cfg）+ runtime/（自带 JRE，零系统 Java 依赖））。
// VerifiedHash：远程安装必过 GitHub 官方 digest（上游实测全量在位），恒
// true；导入件无官方摘要可核，如实 false。
type TermoraVersionInfo struct {
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

// DownloadProgress 下载进度事件载荷（事件键 termora:version-download）。
// Stage 词表与全仓托管家族一致：downloading/verify/extract/done/error。
type DownloadProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}
