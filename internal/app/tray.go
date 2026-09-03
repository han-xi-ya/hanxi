// 托盘右键菜单动态装配：条目来自 settings.Store 的 TrayMenu 配置，
// 配置保存后由 AppService 触发 Rebuild 热更新（Wails beta.10 的 SetMenu
// 经 InvokeSync 在 UI 线程 destroy+recreate 原生菜单，重复调用安全）。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/notify"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// trayMenuBuilder 负责按当前配置组装托盘右键菜单并分发点击动作。
type trayMenuBuilder struct {
	app      *application.App
	win      *application.WebviewWindow
	tray     *application.SystemTray
	registry *extapi.Registry
	store    *settings.Store
}

func newTrayMenuBuilder(a *application.App, win *application.WebviewWindow, tray *application.SystemTray, registry *extapi.Registry, store *settings.Store) *trayMenuBuilder {
	return &trayMenuBuilder{app: a, win: win, tray: tray, registry: registry, store: store}
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
	if b.store != nil {
		for _, item := range b.store.GetTrayMenu() {
			if item.Enabled {
				configured = append(configured, item)
			}
		}
	}

	if len(configured) > 0 {
		menu.AddSeparator()
		for _, item := range configured {
			menu.Add(b.resolveLabel(item)).OnClick(func(ctx *application.Context) {
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

// resolveLabel 优先取用户自定义名，缺省回退到命令默认标签 / 导航标题 / 程序文件名。
func (b *trayMenuBuilder) resolveLabel(item settings.TrayMenuItem) string {
	if label := strings.TrimSpace(item.Label); label != "" {
		return label
	}
	switch item.Type {
	case settings.TrayItemCommand:
		for _, cmd := range b.registry.ListTrayCommands() {
			if cmd.Key == item.Ref {
				return cmd.Label
			}
		}
	case settings.TrayItemRoute:
		for _, nav := range b.registry.GetEnabledNavs() {
			if nav.Route == item.Ref {
				return nav.Title
			}
		}
	case settings.TrayItemExe:
		if base := filepath.Base(item.Path); base != "." && base != string(filepath.Separator) {
			return strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return item.Ref
}

// dispatch 按条目类型分发点击动作；耗时操作一律进 goroutine，避免阻塞托盘回调。
func (b *trayMenuBuilder) dispatch(item settings.TrayMenuItem) {
	switch item.Type {
	case settings.TrayItemCommand:
		go b.runCommand(item)
	case settings.TrayItemRoute:
		b.showAndFocus()
		b.app.Event.Emit("tray:navigate", item.Ref)
	case settings.TrayItemExe:
		go b.runExe(item)
	default:
		b.notifyError(item, fmt.Errorf("未知的托盘条目类型: %q", item.Type))
	}
}

// runCommand 执行托管模块的托盘命令（registry 内部先完成懒初始化）。
func (b *trayMenuBuilder) runCommand(item settings.TrayMenuItem) {
	if err := b.registry.RunTrayCommand(context.Background(), item.Ref); err != nil {
		slog.Warn("tray: run command failed", "ref", item.Ref, "err", err)
		b.notifyError(item, err)
	}
}

// runExe 启动任意外部程序。刻意不加 JobObject：用户从托盘拉起的桌面应用
// 生命周期独立于 Hanxi，退出 Hanxi 不应连带终止它们。
func (b *trayMenuBuilder) runExe(item settings.TrayMenuItem) {
	path := strings.TrimSpace(item.Path)
	if path == "" {
		b.notifyError(item, fmt.Errorf("未配置程序路径"))
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		b.notifyError(item, fmt.Errorf("程序不存在: %s", path))
		return
	}

	cmd := exec.Command(path, splitArgs(item.Args)...)
	if dir := filepath.Dir(path); dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("tray: launch exe failed", "path", path, "err", err)
		b.notifyError(item, err)
	}
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
		Message:  fmt.Sprintf("%s：%v", b.resolveLabel(item), err),
		Level:    notify.LevelError,
	})
}

// splitArgs 将参数字符串拆为 argv：空白分隔，支持成对双引号包裹含空格的路径；
// 不识别转义符（Windows 启动参数场景够用，避免引入 shell 带来的注入风险）。
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote := false
	hasToken := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			hasToken = true
		case (r == ' ' || r == '\t') && !inQuote:
			if hasToken {
				args = append(args, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if hasToken {
		args = append(args, cur.String())
	}
	return args
}
