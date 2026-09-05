// Package quickmenu 鼠标快捷菜单模块（Quicker 式最小验证）：
// 任意界面右键长按（默认 450ms、松手前即弹）→ 光标处弹出无边框快捷菜单 → 点击条目即时启动。
//
// 设计取舍（验证期）：
//   - 条目与托盘右键菜单完全共用 settings.TrayMenu 配置与 internal/launcher 分发，
//     不新增第二份配置面；验证通过后再演进独立条目模型（图标/分组/条件菜单）；
//   - 全局钩子为进程内低级钩子（WH_MOUSE_LL，非注入），随模块停用/进程退出由
//     系统自动摘除，零残渣；识别采用"吞按下、短按 SendInput 回放"策略保证普通
//     右键零损失（为何不能"吞抬起放按下"，见 TROUBLESHOOTING #29）；
//   - 弹窗为常驻隐藏的单例 frameless WebView 窗口，失焦收起 + 全局点击观察兜底
//     （前台锁下可能抢不到焦点），多显示器/屏幕边缘经物理↔DIP 换算与工作区钳位。
package quickmenu

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

const ID = "quickmenu"

type Module struct {
	svc         *QuickMenuService
	initialized bool
}

// New 返回具体 *Module（而非 extapi.Module 接口）：装配根需要 SetMainWindow 回填主窗引用。
func New(store *settings.Store, registry *extapi.Registry) *Module {
	return &Module{svc: NewQuickMenuService(store, registry)}
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "快捷菜单",
		Version:     "0.1.0",
		Description: "任意处右键长按唤出快捷启动菜单（条目与托盘配置共用）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "快捷菜单",
		Route:   "/ext/quickmenu",
		Icon:    "🖱",
		Section: extapi.SectionExt,
		Order:   90,
	}}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission { return nil }

func (m *Module) Protocol() int { return 1 }

func (m *Module) OnInit(ctx context.Context) error {
	if err := m.svc.start(); err != nil {
		return err
	}
	m.initialized = true
	return nil
}

func (m *Module) OnDestroy() error {
	m.initialized = false
	return m.svc.stop()
}

func (m *Module) IsInitialized() bool { return m.initialized }

// SetMainWindow 透传装配根注入的主窗口引用（route 条目需要唤出主窗口）。
func (m *Module) SetMainWindow(win *application.WebviewWindow) {
	m.svc.SetMainWindow(win)
}
