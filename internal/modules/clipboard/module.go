package clipboard

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/settings"
)

// ID 是模块注册键：同时用作通知/事件的 moduleID、调用门租约键、数据目录键
// （<DataDir>/clipboard/）与 MCP 授权门第十键名（access.json "clipboard"，
// memo 键先例，登记契约 §12.2 收口清单）。范式逐字对齐 memo.ID——收口阶段
// catalog 注册/导航入口/mcpwizard 授权键以本常量为准，共享文件本线一律不碰。
const ID = "clipboard"

// Module 剪贴板历史模块。装配（New 进 catalog、路由/导航注册）归收口主控，
// 本文件只提供与 memo.Module 同谱的模块形态件。
type Module struct {
	svc *ClipboardService
}

// New 实例化模块：构造期装载历史（坏库在 store 内隔离取证副本后空库启动）；
// 装配期即可能失败的模块之一（数据目录不可建/不可读），返回错误时注册表
// 跳过该模块，不影响其余装配。
func New(paths *settings.Paths) (extapi.Module, error) {
	svc, err := NewClipboardService(paths, extapi.NewLeaseHolder(ID))
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务 RPC 经该门取 operation lease（Wave 3 调用门口径同 memo）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// GetService 获取底层 Service 引用（方便跨模块直接交互与收口接线）。
func (m *Module) GetService() *ClipboardService {
	return m.svc
}

// SetHotkeyRegistry 装配根注入全仓通用热键注册器（msgboard/memo 同款接缝）。
// 注册器交接时序先于 OnInit/EnsureActive，Module 不进前端绑定面，注入通道
// 对绑定生成零足迹。
func (m *Module) SetHotkeyRegistry(r *hotkey.Registry) { m.svc.setHotkeyRegistry(r) }

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "剪贴板历史",
		Version:     "0.1.0",
		Description: "系统剪贴板全量历史：文本/图片/文件三类捕获、DPAPI 加密落盘、去重置顶与容量自钳制，全局热键浮层随处选取（首版仅文本回填系统剪贴板）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定效率组内排序；Order 37 紧随随手记 36，
// 最终以收口阶段全表对位为准）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "剪贴板",
		Icon:    "i:clipboard",
		Route:   "/ext/clipboard",
		Section: extapi.SectionExt,
		Order:   37,
		Group:   extapi.GroupEfficiency,
	}}
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向托盘/轮盘命令候选
// 目录暴露「唤出浮层」「暂停/恢复记录」两件（msgboard 挂出/撤下同款对开形——
// 托盘菜单只在配置保存时重建，无动态标签机制，标签恒定双态并写）。
//
// 「一键擦除」刻意**不做成命令**：机主裁决（契约 §12 v1.5.1）为"跳回视图复用
// 前端富文案确认闸"——Nav route 注册后「打开剪贴板页」已由 ListTrayMenuOptions
// 自动聚合为托盘候选（elevate_tray.go 聚合面），页面内全清按钮携带条数+blob
// 话术的 useConfirm 闸即唯一擦除入口，不另造 Go 侧盲擦通道、不加第五事件。
//
// 命令体经 registry.RunTrayCommand 先行 Acquire 持租约后调用，service 面
// holder.Enter 嵌套计数实证可行（msgboard Toggle 同谱，wiring-checklist §5.1）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{
			ID:    "overlay",
			Label: "唤出剪贴板浮层",
			Run: func(context.Context) error {
				return m.svc.toggleOverlay()
			},
		},
		{
			ID:    "toggle-pause",
			Label: "暂停/恢复记录",
			Run: func(context.Context) error {
				st, err := m.svc.GetStatus()
				if err != nil {
					return err
				}
				return m.svc.SetPaused(!st.Paused)
			},
		},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；历史数据已在 New 时
// 装载，生命周期做监听消息泵与浮层热键的绑/摘、浮层窗收口（有副作用，非空钩子）。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// OnInit 起剪贴板监听并按默认键位绑定全局浮层热键（失败一律降级只记日志，
// 不阻塞启用）。
func (m *Module) OnInit(ctx context.Context) error {
	return m.svc.start()
}

// OnDestroy 摘监听、摘热键并真销毁浮层（应用退出/模块停用统一收口，
// 不留孤儿消息泵、按键黑洞与白烧的 WebView2 视图）。
func (m *Module) OnDestroy() error {
	return m.svc.stop()
}

// IsInitialized 装配期已完成初始化（New 失败则根本不会注册），恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
