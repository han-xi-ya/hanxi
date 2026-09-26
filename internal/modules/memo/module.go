// Package memo 内置模块：极轻量本地备忘录/代码片段站。
// 数据以每条一文件的 md 库（<数据根>/memo/<id>.md，frontmatter + 正文）持久化，
// NewMemoService 构造时全量装载进内存；旧版整库 state/memo.json 在启动时幂等迁移
// （见 migrate.go），迁移未成的异常态回落旧库读写。
// 悬浮速记卡（N16 B 批）：全局热键（槽位 memo/quicksheet，见 hotkey.go）随处唤出
// 极简速记窗（见 quicksheet.go），落笔走既有 Create 链路，不动存储 schema；
// 偏好（热键键位）独立落 state/memo-prefs.json（见 prefs.go），与便签数据/旧库
// 残片收口互不牵连。除按需建/销的速记窗与空闲销毁计时器外，无常驻 goroutine 与网络面。
package memo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "memo"

// Module 随手记模块
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

// SetHotkeyRegistry 装配根注入全仓通用热键注册器（msgboard 同款接缝，
// 见 app.go）。注册器交接时序先于 OnInit/EnsureActive，Module 不进前端
// 绑定面，注入通道对绑定生成零足迹。
func (m *Module) SetHotkeyRegistry(r *hotkey.Registry) { m.svc.setHotkeyRegistry(r) }

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "随手记",
		Version:     "0.3.0",
		Description: "极轻量本地持久化备忘录与临时代码片段站，支持 Markdown 便签、速记、标签云、置顶、敏感脱敏、一键全删与全局热键悬浮速记卡",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定效率组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "随手记",
		Icon:    "i:sticky-note",
		Route:   "/ext/memo",
		Section: extapi.SectionExt,
		Order:   36,
		Group:   extapi.GroupEfficiency,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；数据已在 New 时加载，
// 生命周期只做速记卡热键的绑/摘与卡窗收口（N16 B 批起有副作用，不再是空钩子）。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// OnInit 按配置绑定全局速记热键（注册失败降级只记日志，不阻塞启用）。
func (m *Module) OnInit(ctx context.Context) error {
	return m.svc.start()
}

// OnDestroy 摘热键并真销毁悬浮速记卡（应用退出/模块停用统一收口，
// 不留按键黑洞与白烧的 WebView2 视图）。
func (m *Module) OnDestroy() error {
	return m.svc.stop()
}

// IsInitialized 装配期已完成初始化（New 失败则根本不会注册），恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
