// Package memo 内置模块：极轻量本地备忘录/代码片段站。
// 数据以每条一文件的 md 库（<数据根>/memo/<id>.md，frontmatter + 正文）持久化，
// NewMemoService 构造时全量装载进内存；旧版整库 state/memo.json 在启动时幂等迁移
// （见 migrate.go），迁移未成的异常态回落旧库读写。无网络与常驻 goroutine。
package memo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "memo"

// Module 极客随手记模块
type Module struct {
	svc *MemoService
}

// New 实例化模块。少数在装配期即可能失败的模块之一（数据文件损坏/不可读），
// 返回错误时注册表跳过该模块，不影响其余装配。
func New(paths *settings.Paths) (extapi.Module, error) {
	svc, err := NewMemoService(paths, extapi.NewLeaseHolder(ID))
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务 RPC 经该门取 operation lease（Wave 3 调用门）。
// 跨模块直调（fileshare 投递→QuickCreate、snapshot 热恢复→RestoreFile）
// 捕获的正是这些导出版：memo 停用时直调被门拒绝并携带明确错误上浮，属预期语义。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// GetService 获取底层 Service 引用 (方便跨模块直接交互)
func (m *Module) GetService() *MemoService {
	return m.svc
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "极客随手记",
		Version:     "0.2.0",
		Description: "极轻量本地持久化备忘录与临时代码片段站，支持 Markdown 便签、速记、标签云、置顶、敏感脱敏与一键全删",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定效率组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "随手备忘录",
		Icon:    "i:sticky-note",
		Route:   "/ext/memo",
		Section: extapi.SectionExt,
		Order:   36,
		Group:   extapi.GroupEfficiency,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；数据已在 New 时加载，生命周期无副作用。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	return nil
}

// IsInitialized 装配期已完成初始化（New 失败则根本不会注册），恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
