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

// MsixState 打包形态"当前用户注册状态 + 本地容器缓存清单 + 孤儿应用数据
// 探测"的合并快照（GetMsixState 返回值；注册状态实时查询无缓存，缓存清单
// 来自版本线磁盘枚举，孤儿探测见 probeMsixOrphanData——只读三道闸口径，
// <族名>.orphan-* 隔离备份不算孤儿）。
// Version 归一化为发布号两段形态（2026.2.0.0 → 2026.2，实证规则见
// normalizeMsixVersion 注释），与版本线列表行同一比较口径。
type MsixState struct {
	Installed     bool                    `json:"installed"`
	Version       string                  `json:"version"`
	PackageFamily string                  `json:"packageFamily"`
	Cache         []version.PackageCached `json:"cache"`
	// OrphanData 上次安装/卸载残留的孤儿应用数据在场且全用户查无注册——
	// 前端打包安装失败回执为孤儿档时据此出「🧹 清理后重试」钮（无孤儿钮不出现）。
	OrphanData bool `json:"orphans"`
	// OrphanPath OrphanData 为真时给出精确目录路径（供确认框点名）；其余为空。
	OrphanPath string `json:"orphanPath,omitempty"`
}

// MsixProgress 打包版安装进度事件 translucenttb:msix-progress 的载荷
// （2026-09-27 机主撞账"装打包版全程无反馈"补通道；同步 RPC 语义不变，
// 事件只喂 UI）。Stage 词表 preparing | downloading | verify-sha256 |
// deploying | done | error（与版本线 Prepare 阶段词同源，deploying/error
// 由服务面补档）；Percent 仅下载段有真值，不确定态（deploying 等）为 0，
// error 档 Message 自带归因后完整话术（孤儿档短语即在此通道出货）。
type MsixProgress struct {
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
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
