// Package quickmenu 鼠标快捷菜单模块（Quicker 式最小验证）：
// 任意界面右键长按（默认 450ms、松手前即弹）→ 光标处弹出无边框快捷菜单 → 点击条目即时启动。
//
// 设计取舍（验证期）：
//   - 条目与托盘右键菜单完全共用 settings.TrayMenu 配置与 internal/launcher 分发，
//     不新增第二份配置面；图标与二级分组（group 条目：轮盘子盘展开、托盘原生
//     子菜单，展开开关 QuickMenuTwoTier 关闭时拍平）已内置，条件菜单仍在观察；
//   - 全局钩子为进程内低级钩子（WH_MOUSE_LL，非注入），随模块停用/进程退出由
//     系统自动摘除，零残渣；识别采用"吞按下、短按 SendInput 回放"策略保证普通
//     右键零损失（为何不能"吞抬起放按下"，见 TROUBLESHOOTING #29）；
//   - 弹窗为常驻隐藏的单例 frameless 真透明 WebView 窗口，呈圆形轮盘：圆盘边缘
//     抗锯齿由页面绘制，方形四角经 GDI 区域裁剪（windows.ClipWindowEllipse）从
//     命中测试中剪掉让点击穿透，失焦收起 + 全局点击按圆判定兜底（前台锁下可能
//     抢不到焦点），多显示器/屏幕边缘经物理↔DIP 换算与工作区钳位。
package quickmenu

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "quickmenu"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc         *QuickMenuService
	initialized bool
}

// New 返回具体 *Module（而非 extapi.Module 接口）：装配根需要 SetMainWindow 回填主窗引用。
func New(store *settings.Store, registry *extapi.Registry) *Module {
	return &Module{svc: NewQuickMenuService(store, registry)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "快捷菜单",
		Version:     "0.2.0",
		Description: "任意处右键长按唤出圆形快捷轮盘（条目与托盘配置共用，支持分组二级子盘）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "快捷菜单",
		Route:   "/ext/quickmenu",
		Icon:    "i:mouse-pointer",
		Section: extapi.SectionExt,
		Order:   90,
		Group:   extapi.GroupDesktop,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error {
	if err := m.svc.start(); err != nil {
		return err
	}
	m.initialized = true
	return nil
}

// OnDestroy 摘除全局鼠标钩子并收起弹窗（svc.stop 同步等待钩子线程退出），错误上抛给注册表。
func (m *Module) OnDestroy() error {
	m.initialized = false
	return m.svc.stop()
}

// IsInitialized 如实反映 OnInit 结果：start（钩子安装/弹窗创建）失败时为 false，注册表可重试初始化。
func (m *Module) IsInitialized() bool { return m.initialized }

// SetMainWindow 透传装配根注入的主窗口引用（route 条目需要唤出主窗口）。
func (m *Module) SetMainWindow(win *application.WebviewWindow) {
	m.svc.SetMainWindow(win)
}
