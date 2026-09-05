// 托盘右键菜单动态装配：条目来自 settings.Store 的 TrayMenu 配置，
// 配置保存后由 AppService 触发 Rebuild 热更新（Wails beta.10 的 SetMenu
// 经 InvokeSync 在 UI 线程 destroy+recreate 原生菜单，重复调用安全）。
// 条目的显示名解析与动作执行统一委托 internal/launcher（与快捷菜单共享语义）。
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/launcher"
	"hanxi/internal/notify"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// trayMenuBuilder 负责按当前配置组装托盘右键菜单并分发点击动作。
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
	}

	if len(configured) > 0 {
		menu.AddSeparator()
		for _, item := range configured {
			menu.Add(b.disp.Label(item)).OnClick(func(ctx *application.Context) {
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
