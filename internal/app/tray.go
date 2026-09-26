// 托盘右键菜单动态装配：条目来自 settings.Store 的托盘账 TrayMenu 配置（轮盘
// 另有独立账本 WheelMenu，两账互不连带），配置保存后由 AppService 触发 Rebuild
// 热更新（Wails beta.10 的 SetMenu 经 InvokeSync 在 UI 线程 destroy+recreate
// 原生菜单，重复调用安全）。条目的显示名解析、动作执行与停用模块可见性收口
// 统一委托 internal/launcher（与快捷菜单共享语义实现）。
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/app/appicons"
	"hanxi/internal/extapi"
	"hanxi/internal/launcher"
	"hanxi/internal/notify"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// trayMenuBuilder 负责按当前配置组装托盘右键菜单并分发点击动作。
// registry 供分发器装配期按模块启停过滤引用条目（可见性收口），可为 nil（此时不过滤）。
type trayMenuBuilder struct {
	app   *application.App
	win   *application.WebviewWindow
	tray  *application.SystemTray
	disp  *launcher.Dispatcher
	store *settings.Store
}

func newTrayMenuBuilder(a *application.App, win *application.WebviewWindow, tray *application.SystemTray, registry *extapi.Registry, store *settings.Store) *trayMenuBuilder {
	b := &trayMenuBuilder{app: a, win: win, tray: tray, store: store}
	// route 条目动作：唤出主窗口并请求前端导航（与固定项"设置…"同一条事件通道）。
	b.disp = launcher.New(registry, store, func(route string) {
		b.showAndFocus()
		a.Event.Emit("tray:navigate", route)
	})
	return b
}

// Rebuild 读取最新配置并重建托盘右键菜单。
func (b *trayMenuBuilder) Rebuild() {
	if b == nil || b.tray == nil {
		return
	}
	b.tray.SetMenu(b.build())
}

// build 组装完整菜单：固定项 + 用户配置项 + 设置/退出。
func (b *trayMenuBuilder) build() *application.Menu {
	menu := b.app.NewMenu()
	menu.Add("显示 " + product.Name).OnClick(func(ctx *application.Context) {
		b.showAndFocus()
	})

	var configured []settings.TrayMenuItem
	if b.disp != nil {
		configured = b.disp.EnabledItems()
		// 可见性收口（Wave 1）：用户开关（EnabledItems）之外，引用了停用模块的
		// route/命令条目也整条滤除——执行侧本就被模块门禁挡住，入口不再残留。
		// 过滤实现住在 launcher，与轮盘径同源复用（一份逻辑，两处生效）。
		configured = b.disp.FilterDisabledModules(configured)
	}

	if len(configured) > 0 {
		menu.AddSeparator()
		for _, item := range configured {
			// group 条目渲染为原生子菜单（beta.10 Windows 实现递归处理 MF_POPUP）；
			// 空组/全禁用组不占位，与轮盘侧 wheelView 的过滤规则一致。
			if item.Type == settings.TrayItemGroup {
				var kids []settings.TrayMenuItem
				for _, ch := range item.Children {
					if ch.Enabled && ch.Type != settings.TrayItemGroup {
						kids = append(kids, ch)
					}
				}
				if len(kids) == 0 {
					continue // 子条目全禁用：整组不占位（后端校验只保证配置期非空）
				}
				sub := menu.AddSubmenu(b.disp.Label(item))
				for _, ch := range kids {
					ki := sub.Add(b.disp.Label(ch))
					if png := appicons.ForMenu(launcher.ItemModuleID(ch)); png != nil {
						ki.SetBitmap(png) // N27 批 C：16px 菜单变体（SetBitmap 原生尺寸透传，展示档会撑行高；无变体恒 nil 回落文字）
					}
					ki.OnClick(func(ctx *application.Context) {
						b.dispatch(ch)
					})
				}
				continue
			}
			mi := menu.Add(b.disp.Label(item))
			if png := appicons.ForMenu(launcher.ItemModuleID(item)); png != nil {
				mi.SetBitmap(png)
			}
			mi.OnClick(func(ctx *application.Context) {
				b.dispatch(item)
			})
		}
	}

	menu.AddSeparator()
	menu.Add("设置…").OnClick(func(ctx *application.Context) {
		b.showAndFocus()
		b.app.Event.Emit("tray:navigate", "/settings")
	})
	menu.Add("退出").OnClick(func(ctx *application.Context) {
		b.app.Quit()
	})
	return menu
}

// dispatch 分发点击动作；耗时操作一律进 goroutine，避免阻塞托盘回调。
func (b *trayMenuBuilder) dispatch(item settings.TrayMenuItem) {
	go func() {
		if err := b.disp.Dispatch(context.Background(), item); err != nil {
			slog.Warn("tray: dispatch failed", "type", item.Type, "ref", item.Ref, "err", err)
			b.notifyError(item, err)
		}
	}()
}

func (b *trayMenuBuilder) showAndFocus() {
	b.win.Show()
	b.win.Focus()
}

// notifyError 托盘条目执行失败时统一走通知 Hub（窗口隐藏时自动落原生 Toast）。
func (b *trayMenuBuilder) notifyError(item settings.TrayMenuItem, err error) {
	notify.GetHub().Emit(&notify.Notification{
		ModuleID: "system",
		Title:    "托盘操作失败",
		Message:  fmt.Sprintf("%s：%v", b.disp.Label(item), err),
		Level:    notify.LevelError,
	})
}
