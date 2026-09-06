// Package version 实现 VS Code 版本管理引擎：双形态分发。
//
// 上游二进制只经微软官方 CDN（update.code.visualstudio.com）分发，GitHub
// releases 仅源码归档——远程列表/下载直链/官方哈希全部走官方 API：
//   - 便携版（portable zip）：versions/vscode_X.Y.Z/ 隔离安装，解压后补建
//     data\ 目录即激活官方便携模式（全部数据自包含，见上游文档）；
//   - 安装版（User Installer, Inno Setup）：静默安装/升级经官方安装器执行，
//     安装位置以 HKCU 卸载注册表 InstallLocation 为准（用户可自定义目录）。
//
// 官方 sha256 仅最新版可得（feed 恒返最新版清单）：latest 走官方哈希四层
// 校验，指定旧版降级 markeron 三层（字节数+CRC32+布局自检），见 manager.go。
package version

// Form 分发形态：portable 免安装归档 / installer 用户安装器。
type Form string

const (
	FormPortable  Form = "portable"
	FormInstaller Form = "installer"
)

// Release 远程可下载的 VS Code 版本（Windows x64）。
type Release struct {
	Version     string `json:"version"`     // 纯语义版本如 1.136.1（VS Code 无 v 前缀）
	Commit      string `json:"commit"`      // 构建 commit（40 位 hex，下载直链含于路径）
	DownloadURL string `json:"downloadUrl"` // 官方 CDN 直链（解析自版本化重定向）
	Size        int64  `json:"size"`        // 字节数（HEAD 实测，防截断）
	AssetName   string `json:"assetName"`   // 如 VSCode-win32-x64-1.136.1.zip / VSCodeUserSetup-x64-1.136.1.exe
	// SHA256 官方哈希（feed 更新清单 sha256hash）。上游仅对"最新版"发布官方
	// 哈希（feed 恒返最新清单），旧版恒为空——下载时降级三层完整性校验。
	SHA256 string `json:"sha256"`
}

// VersionInfo 本地便携版已安装版本信息（versions/vscode_X.Y.Z/）。
type VersionInfo struct {
	Version     string `json:"version"`     // 如 1.136.1
	ExePath     string `json:"exePath"`     // Code.exe 完整路径（版本隔离目录根）
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // Code.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
	Verified    bool   `json:"verified"`    // 安装时是否经官方 sha256 校验（仅最新版可得）
}

// InstalledInfo 安装版（本机唯一一份，含用户自行安装的）探测信息。
// 安装位置与版本均来自 HKCU 卸载注册表（用户安装时可选任意目录，
// 实测存在自定义到非默认位置的情况，路径必须以注册表为准）。
type InstalledInfo struct {
	Installed bool   `json:"installed"` // 注册表命中且 Code.exe 存在
	Version   string `json:"version"`   // DisplayVersion（如 1.136.1，缺省回退 PE 资源）
	Dir       string `json:"dir"`       // InstallLocation（规范化，无尾分隔符）
	ExePath   string `json:"exePath"`   // <dir>\Code.exe
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Form    string `json:"form"`    // portable/installer
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/install/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
