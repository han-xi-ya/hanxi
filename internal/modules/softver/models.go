// Package softver 软件版本检测（BACKLOG F5）：面向日常装机软件的"本机版本 ×
// 官方最新版"对照与目录空间勘察，微信是首个跟踪目标（结构按多目标预留扩展位，
// 但不做全量软件列表——全量清单与卸载归 bcu，本模块只管白名单跟踪对象的
// 版本跟踪 + 升级引导 + 空间勘察）。
//
// 口径设计（复用 envcheck 的"本机×官方双口径"成熟模型）：
//   - 本机版本双口径：注册表 Uninstall 键（安装器口径）与 PE 文件版本资源
//     （安装内容口径）并示标源，两者不一致时以版本段更完整者为主口径；
//   - 官方版本：抓官方更新页 SSR HTML 正则提"版本号+下载直链"双结果，
//     解析失败绝不猜测——降级为"打开官方页+显示本机版本"；
//   - 目录大小：数据目录常达几十 GB，扫描一律异步 + 可取消 + 进度事件 +
//     结果缓存（范式抄 wsl 磁盘体检，不仿 envcheck 的同步轻探测）。
package softver

// VersionSource 本机版本的一个读数（标源）。
// Kind 取 registry|pe；Primary 标记本安装的主口径（参与官方对比的那个值）。
type VersionSource struct {
	Kind    string `json:"kind"`    // registry | pe
	Label   string `json:"label"`   // 人类可读标源：注册表 Uninstall / PE 文件版本信息
	Field   string `json:"field"`   // DisplayVersion / FileVersion / ProductVersion
	Value   string `json:"value"`   // 版本读数（空 = 该口径未取到，不出现在列表里）
	Detail  string `json:"detail"`  // 证据位置：注册表键全路径或 exe/dll 全路径
	Primary bool   `json:"primary"` // 是否为主口径读数
}

// LocalInstall 本机一套微信安装（注册表命中一条即一套；3.x 与 4.x 可共存）。
type LocalInstall struct {
	ID               string          `json:"id"`               // 稳定键：weixin|wechat（按注册表子键名）
	Generation       string          `json:"generation"`       // 4.x (Weixin) / 3.x (WeChat) / 未知
	DisplayName      string          `json:"displayName"`      // 注册表 DisplayName 原值（如"微信"）
	Publisher        string          `json:"publisher"`        // 注册表 Publisher
	RegistryKey      string          `json:"registryKey"`      // 命中的 Uninstall 子键全路径
	DisplayVersion   string          `json:"displayVersion"`   // 注册表 DisplayVersion（可空）
	InstallDir       string          `json:"installDir"`       // InstallLocation（回落探测结果，可空）
	InstallDirOrigin string          `json:"installDirOrigin"` // registry | hkcu | default（安装目录证据来源）
	InstallDirExists bool            `json:"installDirExists"`
	EstimatedSizeKB  int64           `json:"estimatedSizeKb"` // 注册表 EstimatedSize（KB，0=无）
	Sources          []VersionSource `json:"sources"`         // 双口径读数（含标源）
	BestVersion      string          `json:"bestVersion"`     // 主口径版本值
	Notes            []string        `json:"notes,omitempty"`
}

// DirSize 目录真实扫描结果（walk 口径，与注册表 EstimatedSize 安装器口径互补）。
type DirSize struct {
	Bytes     int64  `json:"bytes"`
	Files     int64  `json:"files"`
	Dirs      int64  `json:"dirs"`
	Skipped   int64  `json:"skipped"` // 无权限/探测失败的条目数（>0 时结果偏小，如实标注）
	ScannedAt string `json:"scannedAt"`
}

// DirSlot 一个目录槽位（安装目录 / 两代数据目录的候选点）。
// ID 由 kind+路径派生（跨次探测稳定，扫描缓存按 ID 挂接）；路径一律后端解析，
// 前端只回传 ID——与 envcheck RevealToolPath 的"不接受任意路径"红线一致。
type DirSlot struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"` // install | data40 | data30
	Label  string   `json:"label"`
	Path   string   `json:"path"`
	Origin string   `json:"origin"` // registry | hkcu | default | documents | driveRoot | config
	Exists bool     `json:"exists"`
	Active bool     `json:"active"` // 数据目录判定在用（含 all_users / wxid_* 账号子目录）
	Size   *DirSize `json:"size,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// OfficialRelease 官方通道读数（更新页解析结果）。
// DownloadURL 为空表示页面改版只解析出版本号（直链失配），前端仍引导打开官方页。
type OfficialRelease struct {
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	PageURL     string `json:"pageUrl"`
	FetchedAt   string `json:"fetchedAt"`
	ParseNote   string `json:"parseNote,omitempty"`
}

// UpdateHint 本机 vs 官方对比结论。Status：latest | available | noLocal | noOfficial。
type UpdateHint struct {
	Status          string `json:"status"`
	LocalVersion    string `json:"localVersion"`
	OfficialVersion string `json:"officialVersion"`
	Message         string `json:"message"`
}

// InstallerFile 一次成功下载的官方安装包落位记录（N38：只搬包到下载目录，
// 不托管安装/启动——装不装、何时装全由用户双击决定）。
// Note 恒为如实的校验声明：微信官方直链从不旁挂摘要值，工具只能核字节数与
// 可执行文件头，绝不冒充"校验通过"。
type InstallerFile struct {
	Path         string `json:"path"`
	FileName     string `json:"fileName"`
	Version      string `json:"version"` // 下载时官方直链对应的版本号
	Bytes        int64  `json:"bytes"`
	DownloadedAt string `json:"downloadedAt"`
	Note         string `json:"note"`
}

// InstallerProgress softver:installer-download 事件载荷（安装包下载进度流）。
// State：downloading | done | canceled | error；done/canceled/error 为终态。
// done 携带 File（供前端直接写回快照，与 dir-scan 同构）。
type InstallerProgress struct {
	State    string         `json:"state"`
	FileName string         `json:"fileName"`
	Done     int64          `json:"done"`
	Total    int64          `json:"total"`
	Message  string         `json:"message,omitempty"`
	File     *InstallerFile `json:"file,omitempty"`
}

// ScanProgress softver:dir-scan 事件载荷（目录大小扫描进度流）。
// State：queued | running | done | canceled | error；done/canceled/error 为终态。
type ScanProgress struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Bytes     int64  `json:"bytes"`
	Files     int64  `json:"files"`
	Dirs      int64  `json:"dirs"`
	Skipped   int64  `json:"skipped"`
	Current   string `json:"current"`
	Message   string `json:"message,omitempty"`
	ScannedAt string `json:"scannedAt,omitempty"`
}

// Snapshot 页面全量数据：本机探测（注册表+PE+目录槽位）× 官方缓存 × 对比结论。
// 官方部分只读缓存（网络取数走 RefreshOfficial，页面进入不隐式发起外呼）。
type Snapshot struct {
	ProbedAt      string           `json:"probedAt"`
	Installs      []LocalInstall   `json:"installs"`
	Dirs          []DirSlot        `json:"dirs"`
	Official      *OfficialRelease `json:"official,omitempty"`
	OfficialError string           `json:"officialError,omitempty"` // 上次官方取数失败原因（降级提示用）
	Update        *UpdateHint      `json:"update,omitempty"`
	Scanning      []string         `json:"scanning"` // 进行中的扫描槽位 ID
	Downloading   bool             `json:"downloading"`
	Downloaded    *InstallerFile   `json:"downloaded,omitempty"` // 最近一次成功下载的安装包
	Notes         []string         `json:"notes,omitempty"`
}
