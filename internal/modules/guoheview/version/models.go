// Package version 实现果核看图（GuoheView）版本管理引擎：官方发布接口
// （rj.lovestu.com，非 GitHub）版本查询、Windows x64 便携 zip 下载（官方 MD5
// 校验）、保布局解压隔离安装与本地导入。
//
// 与 GitHub releases 家族的差异（上游实证结论，勿照抄模板）：
//   - 发布接口每次仅返回"当前版本"（stable/beta 两个 channel 各一），
//     无历史版本列表——远程表至多两条，回滚只能靠 ImportLocal 导入本地已有目录；
//   - 官方哈希只提供 MD5（无 sha256），完整性第一层以其为准；
//   - 版本号是四段式 3.2.7.98（version + version_code），非纯语义三段式。
package version

// ViewRelease 官方发布接口的一个可用版本（stable 或 beta 通道）。
// MD5/Size 来自接口 files 数组（官方计算，完整性校验第一依据）。
// 托管统一使用便携 zip 资产：根目录即 portable.ini 全便携布局，配置随目录走；
// 安装包 exe（Inno/NSIS 系）与 .7z（需外部解压依赖）均不适合隔离托管。
type ViewRelease struct {
	Version   string `json:"version"`   // 归一化四段版本，如 v3.2.7.98
	Channel   string `json:"channel"`   // stable / beta
	IsPre     bool   `json:"isPre"`     // beta 通道即预发布
	AssetName string `json:"assetName"` // 如 GuoheView_v3.2.7.98-便携版.zip
	AssetURL  string `json:"assetUrl"`  // 官方直链（无镜像）
	Size      int64  `json:"size"`      // 资产字节数
	MD5       string `json:"md5"`       // 官方 MD5（hex 小写）
}

// ViewVersionInfo 本地已安装的果核看图版本信息。
// 有效载荷是整个解压目录（exe + core-ui.dll/ghde.dll 解码内核 + resources/ +
// plugins/ + portable.ini），配置 config.ini 与 exe 同目录（portable.ini 激活
// 便携模式），版本隔离目录即完整便携实例。
type ViewVersionInfo struct {
	Version     string `json:"version"`     // 如 v3.2.7.98
	ExePath     string `json:"exePath"`     // GuoheView.exe 完整路径
	Dir         string `json:"dir"`         // 版本隔离目录
	Size        int64  `json:"size"`        // GuoheView.exe 大小（字节）
	InstalledAt string `json:"installedAt"` // 安装时间 yyyy-MM-dd HH:mm:ss
	IsImport    bool   `json:"isImport"`    // 是否经「导入本地」安装（非远程下载）
	Source      string `json:"source"`      // 导入来源目录（仅导入安装时有值）
}

// DownloadProgress 下载过程实时进度。
type DownloadProgress struct {
	Version string `json:"version"` // 目标版本
	Stage   string `json:"stage"`   // resolve/downloading/verify/extract/done/error
	Done    int64  `json:"done"`    // 已下载字节
	Total   int64  `json:"total"`   // 总字节（未知为 0）
	Message string `json:"message"` // 附加信息（如错误描述）
}
