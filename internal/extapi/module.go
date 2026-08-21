// Package extapi 定义模块契约（所有能力模块的统一定义）。
//
// HubKit 是"工具箱"：所有能力（frpc 联调、局域网扫描、释放端口、公网 IP）都是模块，
// 统一注册、统一启停（enabled 开关）、统一注入导航与服务。
// 模块分两级：
//   - LevelBuiltin：编译进主程序，仅可启停（MVP 全部为此级）；
//   - LevelExternal：未来可拆为独立子进程插件（manifest + JSON-RPC over stdio），
//     支持独立安装/卸载（P1+，接口已按此形态预留）。
//
// 当前耦合说明：Service 类型直接复用 wails 的 application.Service
// （内建模块是 GUI 应用的一部分，此耦合可接受；若未来做子进程插件，
// 数据面会改为 JSON-RPC，界面入口由宿主提供）。
package extapi

import "github.com/wailsapp/wails/v3/pkg/application"

// Service 是 wails 服务的别名，避免扩展包直接依赖 wails。
type Service = application.Service

// NewService 包装一个具体类型的 service 实例为 Service。
// 泛型由调用点（扩展包）提供具体类型，wails 的静态分析器可识别。
func NewService[T any](instance *T) Service {
	return application.NewService(instance)
}

// NavSection 区分核心导航与扩展导航两个分区。
type NavSection string

const (
	SectionCore NavSection = "core"
	SectionExt  NavSection = "ext"
)

// NavEntry 描述左侧导航中的一项（前端据此注册路由）。
type NavEntry struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Route   string     `json:"route"`
	Icon    string     `json:"icon"`
	Section NavSection `json:"section"`
	Order   int        `json:"order"`
}

// Level 模块级别。
type Level string

const (
	// LevelBuiltin 编译进主程序的内建模块，可启停但不可卸载。
	LevelBuiltin Level = "builtin"
	// LevelExternal 外部子进程模块（未来），可通过模块管理页独立安装/卸载。
	LevelExternal Level = "external"
)

// ModuleInfo 模块元信息。
type ModuleInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Level       Level  `json:"level"`
	Removable   bool   `json:"removable"` // 仅 LevelExternal 为 true
	Enabled     bool   `json:"enabled"`
}

// Permission 声明扩展所需的能力。运行期由宿主白名单校验，
// 扩展不得绕过宿主 API（host）直接操作系统资源。
type Permission string

const (
	PermKillProcess Permission = "kill-process" // 结束进程：宿主强制 PID 复核 + 保护规则
	PermLANScan     Permission = "lan-scan"     // 局域网探测
	PermNetwork     Permission = "network"      // 出站网络访问
)

// Module 是扩展契约。
type Module interface {
	// Info 返回元信息；ID 必须全局唯一。
	Info() ModuleInfo
	// Nav 返回注册到左侧导航的条目（未启用时不展示）。
	Nav() []NavEntry
	// Services 返回已包装好的 wails service（用 extapi.NewService 包装）。
	Services() []Service
	// Permissions 声明需要的能力，宿主在启用时核对白名单。
	Permissions() []Permission
	// Protocol 返回扩展契约版本，未来子进程插件用于版本握手。
	Protocol() int
}