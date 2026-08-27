package detect

// Status 单个工具的探测状态。
type Status string

const (
	StatusInstalled Status = "installed"  // 已安装且版本解析成功
	StatusMissing   Status = "missing"    // PATH 中找不到该工具
	StatusError     Status = "error"      // 找到但执行失败或版本输出无法识别
	StatusStoreStub Status = "store-stub" // WindowsApps 商店存根（假 python 等）
)

// ToolInfo 单个工具的探测结果（Wails 序列化给前端的 JS 模型）。
type ToolInfo struct {
	Name    string `json:"name"`    // 注册名（兼可执行文件名）：git/node/java/...
	Display string `json:"display"` // 前端展示名：Git / Node.js / ...
	Path    string `json:"path"`    // exec.LookPath 命中的绝对路径（未安装时为空）
	Version string `json:"version"` // 解析出的版本号（未安装/失败时为空）
	Status  Status `json:"status"`
	Hint    string `json:"hint"` // 面向用户的状态说明（installed 时为空）
}