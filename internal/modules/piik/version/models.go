// Package version 实现 Piik 版本管理引擎：GitHub Releases 远程列表、
// 便携 zip 下载（官方 sha256 校验，委托共享内核 artifact.Fetch）、平铺
// 自管目录解包落位（内核 Tree）、账外漂移护栏与本地整套导入。
//
// 上游纪律（TNTcraftHIM/Piik，MIT，屏幕分享 headless 本地服务器，阶段 0 实证）：
//   - 发行形态：Go 主程序 + 内嵌 Web UI，浏览器开 http://127.0.0.1:<port>/ 即用；
//   - 稳定版 tag 恒 vX.Y.Z（下限 ≥v1.1.0，更早无托管所需机读模式）；Windows x64
//     便携资产恒裸名 piik-app-windows-amd64.zip（19 连版实测稳定，不含版本段）；
//   - 信任根唯一：GitHub API 资产 digest（sha256）。上游 .sha256 sidecar 自
//     v1.2.0 起停发，任何校验链不得依赖它——digest 缺失的 release 不入列表、
//     拒无校验安装，宁拒不猜；
//   - zip 根平铺十条目：piik-app.exe（约 46MB）+ LICENSE/REVISION/NOTICES +
//     runtime/native/piik-capture.exe + runtime/tunnel/cloudflared.exe 等。
//     兄弟路径 exe 按"exe 所在目录"相对解析，目录结构不可拆——布局自检
//     因此钉死五关键条目在场 + runtime 双 exe 在位；
//   - 无单实例 mutex（8787 端口绑定即事实互斥）、无托盘、无 quit CLI；提权
//     manifest asInvoker；数据默认 %APPDATA%\Piik\client.json——托管改道经
//     CLI --config/--log-dir 注入（instance 引擎组参），本包只管资产不管运行。
//
// 完整性策略（四层，DBX 同构）：①GitHub API digest 官方摘要（唯一信任根）
// ②声明字节数与流式上限双核（内核 artifact.Fetch）③ZipSlip/炸弹/CRC32 全量
// 解包闸门（内核 artifact.UnpackZip）④落位布局自检（五关键条目 + runtime
// 双 exe，模块策略内核不感知）+ 落位 exe sha256 记账 + mtime 闸控复算的
// 账外漂移护栏（照 everything/DBX 实作；上游无自更新器，漂移属旁证性护栏）。
package version

import "hanxi/packages/go/hostfeed"

// PiikRelease 远程 GitHub Release 中可用的 Piik Windows x64 便携版。
// SHA256 来自 GitHub API 资产 digest（官方计算，完整性校验的唯一依据；
// 上游 .sha256 sidecar 自 v1.2.0 起停发，不作为任何降级参照）。
type PiikRelease struct {
	Version   string `json:"version"`   // 如 v1.2.0（tag 原样带 v 前缀）
	Published string `json:"published"` // 发布时间（RFC3339）
	AssetName string `json:"assetName"` // 恒 piik-app-windows-amd64.zip（裸名）
	AssetURL  string `json:"assetUrl"`  // 资产下载地址（302 到 CDN）
	Size      int64  `json:"size"`      // 资产大小（字节）
	SHA256    string `json:"sha256"`    // 官方 sha256（digest 去掉前缀）
	// Assets 上游全发布物平台/形态矩阵（N13 展示层，纯展示，下载路径不消费）。
	Assets []hostfeed.AssetNote `json:"assets,omitempty"`
	// Form 本托管资产形态（N13 形态标注，纯展示，ListRemote 回填）：恒
	// portable——piik-app-windows-amd64.zip 解压即跑（根平铺 + runtime 子树，
	// 无任何安装器形态）。资产名无便携字样，机械判名 Classify 会保守降为
	// archive，形态以实测布局自证为准。
	Form hostfeed.Form `json:"form,omitempty"`
}

// hostedForm 本模块托管形态事实（全家族经 manager.ListRemote 回填进 Release.Form）。
const hostedForm = hostfeed.FormPortable

// PiikVersionInfo 本地已安装的 Piik 版本信息。
// SHA256 为账本内主 exe 落位摘要（Download/ImportLocal 成功时实测记入）；
// VerifiedHash：远程下载链经官方摘要校验为 true，本地导入无官方参照为 false。
// HashDrifted 为账外漂移护栏：ListInstalled 刷新时对"落位后 mtime 变新"的
// 主 exe 复算摘要，与账本不符即如实标记——只报告不处置，不撒谎（成本控制
// 见 driftCheck 注释；piik 上游无自更新器，漂移多属手工换件/磁盘异常，
// runtime 双 exe 不在复算面内，与家族同口径）。
type PiikVersionInfo struct {
	Version      string `json:"version"`      // 如 v1.2.0
	ExePath      string `json:"exePath"`      // piik-app.exe 完整路径（供 instance 引擎拉起）
	Dir          string `json:"dir"`          // 版本隔离目录（平铺布局根，兄弟路径解析依赖整目录）
	Size         int64  `json:"size"`         // 版本目录整树字节和（度量失败回退主 exe 大小）
	InstalledAt  string `json:"installedAt"`  // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport     bool   `json:"isImport"`     // 是否经「导入本地」安装（非远程下载）
	Source       string `json:"source"`       // 导入来源目录（仅导入安装时有值）
	SHA256       string `json:"sha256"`       // 账本内主 exe 落位摘要（空=账本无摘要）
	VerifiedHash bool   `json:"verifiedHash"` // 安装时是否经官方摘要校验
	HashDrifted  bool   `json:"hashDrifted"`  // 落位后主 exe 是否发生账外漂移
	DriftNote    string `json:"driftNote"`    // 漂移复查明细（如实转述，含无法比对场景）
	Revision     string `json:"revision"`     // REVISION 记账（上游构建标识；漂移复算不覆盖本字段）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
