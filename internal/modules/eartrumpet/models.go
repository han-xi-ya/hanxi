package eartrumpet

import (
	"path/filepath"
	"strings"

	"hanxi/internal/platform/apppackage"
)

// EarTrumpet 存在两个官方发行渠道（Publisher 不同 → PFN 不同）。Hanxi 只
// 管理官方直装渠道（install.eartrumpet.app，可自动更新、ACS 签名）；商店
// 渠道不再纳管，仅保留注册检测用于并存警告——两渠道共享
// Local\{程序集名}-{GUID} 单实例互斥体，同时注册时同一时刻只有一个实例能
// 运行且配置互相独立（各自的 LocalSettings 容器）。
//
// 身份常量 2026-09-03 实机核实：直装 PFN/Publisher 与 winget-pkgs 官方清单
// 及线上 appinstaller XML 双向核对；商店 PFN 来自当时实装的 Get-AppxPackage。
// AppID 两渠道一致为 "EarTrumpet"（非 "App"）。
const (
	PackageName = "40459File-New-Project.EarTrumpet"
	MainAppID   = "EarTrumpet"
	repoURL     = "https://github.com/File-New-Project/EarTrumpet"

	// exeName 两渠道与 loose 版共用的主进程映像名。
	exeName = "EarTrumpet.exe"
)

var (
	// managedIdentity 是唯一纳管身份：官方直装渠道。
	managedIdentity = apppackage.Identity{
		Name:      PackageName,
		Family:    PackageName + "_725pr5jq8wr8a",
		Publisher: "CN=File-New-Project, O=File-New-Project, L=Purcellville, S=Virginia, C=US",
		AppID:     MainAppID,
	}
	// storeIdentity 不纳管，仅用于检测商店版并存。
	storeIdentity = apppackage.Identity{
		Name:      PackageName,
		Family:    PackageName + "_1sdd7yawvg6ne",
		Publisher: "CN=6099D0EF-9374-47ED-BDFE-A82136831235",
		AppID:     MainAppID,
	}
)

// isUnderDir 判断 path 是否位于 dir 之下（大小写不敏感），用于把进程精确归属
// 到某个包安装目录（区分 Store/直装两个渠道）。
func isUnderDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(strings.ToLower(rel), ".."+string(filepath.Separator))
}

// PackageSnapshot 是直装渠道的包状态快照；StoreCoexist 标记商店版并存。
type PackageSnapshot struct {
	ObservedAt      string `json:"observedAt"`
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	Version         string `json:"version"`
	PackageFullName string `json:"packageFullName"`
	Architecture    string `json:"architecture"`
	InstallLocation string `json:"installLocation"`
	PackageStatus   string `json:"packageStatus"`
	StoreCoexist    bool   `json:"storeCoexist"`
	StoreVersion    string `json:"storeVersion"`
}
