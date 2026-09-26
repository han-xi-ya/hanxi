package translucenttb

import "hanxi/internal/modules/translucenttb/version"

// TranslucentTB 官方 MSIX 打包形态的固定身份四元组（装配见 service.go msixIdentity，
// 安装/查询/卸载/激活全部以此为准）。
//
// 实证来源（2026-09 三源交叉，非猜测）：
//   - 上游 CI .azp/scripts/update-manifest.ps1：BuildType=Release 把包名改写为
//     28017CharlesMilette.TranslucentTB、Publisher 改写为 CN=04797BBC-C7BB-462F-9B66-331C81E27C0E；
//   - 真实产物 2026.2 bundle.msixbundle（sha256 对过 GitHub API digest）内
//     AppxBundleManifest/AppxManifest 的 Identity 与 Application Id；
//   - winget-pkgs 清单（2024.3/2025.1/2026.1 一致）：
//     PackageFamilyName 与 DefaultInstallLocation（WindowsApps 全名）双重印
//     _v826wp6bftszj 后缀；InstallerSha256 与 GitHub API digest 同字节。
//
// 后缀采用常量而非运行时从 bundle 解析：manifest 未声明 PublisherId，
// _v826wp6bftszj 是 Windows 部署时从签名证书 subject 推导的哈希，bundle 内
// 任何文件都不含该字面量，解析不可得。上游换签名主体（此 GUID 不变即稳定，
// 已跨 Store 与 Azure Trusted Signing 两代证书主体沿用）则整条线随
// Add-AppxPackage 部署校验一并显性失败，不做静默兜底。
const (
	MsixPackageName   = "28017CharlesMilette.TranslucentTB"
	MsixPackageFamily = "28017CharlesMilette.TranslucentTB_v826wp6bftszj"
	MsixPublisher     = "CN=04797BBC-C7BB-462F-9B66-331C81E27C0E"
	MsixMainAppID     = "TranslucentTB"
)

// MsixState 打包形态"当前用户注册状态 + 本地容器缓存清单"的合并快照
// （GetMsixState 返回值；注册状态实时查询无缓存，缓存清单来自版本线磁盘枚举）。
// Version 归一化为发布号两段形态（2026.2.0.0 → 2026.2，实证规则见
// normalizeMsixVersion 注释），与版本线列表行同一比较口径。
type MsixState struct {
	Installed     bool                    `json:"installed"`
	Version       string                  `json:"version"`
	PackageFamily string                  `json:"packageFamily"`
	Cache         []version.PackageCached `json:"cache"`
}

// ControlOutcome 启动/重设状态操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ already-running（自有实例）/ external-detected（外部实例已在运行）/ starting（启动竞态中）/ reset-sent（自有实例信使重设）/ external-reset（外部实例信使重设）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}
