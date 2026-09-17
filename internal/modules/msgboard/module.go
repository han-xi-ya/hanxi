// Package msgboard 桌面留言板（参考 MooTool messageBoard）：离开工位一键在显示器
// 全屏挂出"马上回来/会议中/请勿动我电脑"式告示牌。
//
// 设计边界（BACKLOG F8 卡片裁定，MVP 克制版）：
//   - 三通道唤起：托盘命令 + 轮盘条目（同一份 settings.TrayMenu 配置、同一个
//     extapi.TrayCommandsProvider 注册点，经 internal/launcher 现成派发）+
//     全局热键（收编入 internal/hotkey 通用注册器槽位，见 hotkey.go）；
//   - 内容 = 内置预设模板若干条 + 自定义文字与字号（jsonstore 原子写落
//     state/msgboard.json）；副屏支持=可选显示器挂单屏牌，默认跟随主屏；
//   - 挂牌期间通过平台层 KeepAwake 引用计数聚合器阻止系统/显示器休眠
//     （屏一熄牌子就白挂），撤牌即释放——聚合器面向所有"我在场别睡"类
//     能力复用，留言板只是第一个持有人；
//   - 明确不做：倒计时自动摘牌、日历/锁屏联动、密码锁屏。
//
// 窗口形态与 quickmenu 轮盘同谱：frameless 真透明 + 置顶 + 不进任务栏，按需
// 创建、撤牌即真销毁（#53 摘 hook→Close 通路，不养隐藏窗烧 WebView2 内存）。
package msgboard

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作通知/事件与防休眠聚合器持有人的 moduleID。
const ID = "msgboard"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc         *MsgBoardService
	initialized bool
}

// New 返回具体 *Module（全局热键开机即待命，装配根需常驻激活本模块）。
func New(plat platform.Platform, paths *settings.Paths) *Module {
	return &Module{svc: NewMsgBoardService(plat, paths)}
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "桌面留言板",
		Version:     "0.1.0",
		Description: "一键全屏挂出离岗告示牌（预设+自定义文案），挂牌期间阻止休眠，托盘/轮盘/热键三通道唤出",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（桌面增强组，紧随快捷菜单之后）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "桌面留言板",
		Route:   "/ext/msgboard",
		Icon:    "i:message-circle",
		Section: extapi.SectionExt,
		Order:   95,
		Group:   extapi.GroupDesktop,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向托盘/轮盘命令候选
// 目录暴露"挂出/撤下留言牌"开关（Key=msgboard/toggle，宿主独立 goroutine 调用，
// 服务层 beginOp 连按防抖）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{{
		ID:    "toggle",
		Label: "挂出/撤下留言牌",
		Run: func(context.Context) error {
			return m.svc.Toggle()
		},
	}}
}

// SetHotkeyRegistry 装配根注入全仓通用热键注册器（quickmenu SetMainWindow 同款
// 接缝；随 application.New 就绪、须在 EnsureActive 前交接，start() 即按配置绑定。
// Module 不进前端绑定面，注入通道对绑定生成零足迹）。
func (m *Module) SetHotkeyRegistry(r *hotkey.Registry) { m.svc.setHotkeyRegistry(r) }

// OnInit 注册全局热键（懒加载生命周期，注册失败降级不阻塞启用）。
func (m *Module) OnInit(ctx context.Context) error {
	if err := m.svc.start(); err != nil {
		return err
	}
	m.initialized = true
	return nil
}

// OnDestroy 摘热键、撤牌并释放防休眠诉求（应用退出/模块停用统一收口）。
func (m *Module) OnDestroy() error {
	m.initialized = false
	return m.svc.stop()
}

// IsInitialized 如实反映 OnInit 结果。
func (m *Module) IsInitialized() bool { return m.initialized }

// Service 暴露服务实例（测试与装配根备用；当前 app.go 仅经注册表驱动）。
func (m *Module) Service() *MsgBoardService { return m.svc }
