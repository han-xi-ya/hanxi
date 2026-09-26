// Package version 实现 Snipaste 官网免安装版的版本发现、完整性校验与隔离安装。
package version

import "hanxi/packages/go/hostfeed"

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
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，下载/校验路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——官网 archives 下 Snipaste-X.Y.Z-x64.zip 即"免安装版"（解压
	// 即用）；资产名无便携字样，机械判名 Classify 保守降为 archive，形态以
	// 官网事实与托管安装链（解包直用）自证为准。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

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
