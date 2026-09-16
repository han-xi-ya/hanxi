package nanazip

// NanaZip 官方 MSIX 包的固定身份四元组：查询/安装/卸载/激活全部以此为准。
// Publisher 是证书指纹（Subject CN），换签名即换身份，升级校验依赖其不变。
const (
	PackageName   = "40174MouriNaruto.NanaZip"
	PackageFamily = "40174MouriNaruto.NanaZip_gnj4mf6z9tkrc"
	Publisher     = "CN=E310A153-74A9-4D81-800B-857A8D58408A"
	MainAppID     = "NanaZip.Modern"
)

// PackageSnapshot 一次"包注册状态 + 进行中操作"的合并快照（前端唯一状态源）。
// Revision 单调递增：前端据此丢弃乱序到达的旧事件。Operation* 字段在无操作时为空。
type PackageSnapshot struct {
	Revision         uint64 `json:"revision"`
	ObservedAt       string `json:"observedAt"`
	Installed        bool   `json:"installed"`
	Version          string `json:"version"`
	PackageFullName  string `json:"packageFullName"`
	PackageFamily    string `json:"packageFamily"`
	Architecture     string `json:"architecture"`
	InstallLocation  string `json:"installLocation"`
	PackageStatus    string `json:"packageStatus"`
	OperationID      string `json:"operationId"`
	OperationKind    string `json:"operationKind"`
	OperationState   string `json:"operationState"`
	LastErrorCode    string `json:"lastErrorCode"`
	LastErrorMessage string `json:"lastErrorMessage"`
}

// OperationProgress 异步包操作（install/uninstall）的进度事件模型。
// Terminal=true 表示终态事件（成功/失败各一次，之后快照失效需重新拉取）；
// Stage 依次为 preflight → downloading/cache-commit（下载阶段）→ installing|uninstalling → 终态。
type OperationProgress struct {
	OperationID   string `json:"operationId"`
	Kind          string `json:"kind"`
	TargetVersion string `json:"targetVersion"`
	Stage         string `json:"stage"`
	Done          int64  `json:"done"`
	Total         int64  `json:"total"`
	Message       string `json:"message"`
	Terminal      bool   `json:"terminal"`
	Success       bool   `json:"success"`
	ErrorCode     string `json:"errorCode"`
	ErrorDetail   string `json:"errorDetail"`
}

// OperationAccepted 异步操作的受理回执：仅表示操作已启动（含 OperationID 供前端关联进度事件），
// 不代表成功；Kind 为 already-installed/already-uninstalled 时表示幂等短路、无后台操作。
type OperationAccepted struct {
	OperationID string `json:"operationId"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
}
