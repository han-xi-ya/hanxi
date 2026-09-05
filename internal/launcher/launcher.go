// Package launcher 统一执行 TrayMenuItem 配置条目：托盘右键菜单与鼠标唤出快捷菜单
// （quickmenu）共享同一份条目语义（显示名解析 + command/route/exe 三类动作分发），
// 避免两套菜单各写一份分发逻辑而漂移。
package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// Dispatcher 按条目类型分发执行动作。耗时条目（command/exe 启动）由调用方决定
// 是否放入 goroutine，本包只保证同步语义与错误回传，不内置通知策略。
type Dispatcher struct {
	registry *extapi.Registry
	store    *settings.Store
	navigate func(route string) // route 条目动作：宿主注入（显示主窗口并导航）
}

// New 构造分发器。registry/store 允许为 nil（对应类型条目执行时报错回退）。
// navigate 为 nil 时 route 条目返回错误而非 panic。
func New(registry *extapi.Registry, store *settings.Store, navigate func(route string)) *Dispatcher {
	return &Dispatcher{registry: registry, store: store, navigate: navigate}
}

// EnabledItems 返回已启用条目的副本（配置保存顺序即展示顺序）。
func (d *Dispatcher) EnabledItems() []settings.TrayMenuItem {
	var out []settings.TrayMenuItem
	if d.store == nil {
		return out
	}
	for _, item := range d.store.GetTrayMenu() {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out
}

// Label 优先取用户自定义名，缺省回退到命令默认标签 / 导航标题 / 程序文件名。
func (d *Dispatcher) Label(item settings.TrayMenuItem) string {
	if label := strings.TrimSpace(item.Label); label != "" {
		return label
	}
	switch item.Type {
	case settings.TrayItemCommand:
		if d.registry != nil {
			for _, cmd := range d.registry.ListTrayCommands() {
				if cmd.Key == item.Ref {
					return cmd.Label
				}
			}
		}
	case settings.TrayItemRoute:
		if d.registry != nil {
			for _, nav := range d.registry.GetEnabledNavs() {
				if nav.Route == item.Ref {
					return nav.Title
				}
			}
		}
	case settings.TrayItemExe:
		if base := filepath.Base(item.Path); base != "." && base != string(filepath.Separator) {
			return strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return item.Ref
}

// Dispatch 同步执行一个条目；调用方按需包 goroutine 以免阻塞 UI 回调线程。
func (d *Dispatcher) Dispatch(ctx context.Context, item settings.TrayMenuItem) error {
	switch item.Type {
	case settings.TrayItemCommand:
		if d.registry == nil {
			return fmt.Errorf("模块注册表不可用")
		}
		return d.registry.RunTrayCommand(ctx, item.Ref)
	case settings.TrayItemRoute:
		if d.navigate == nil {
			return fmt.Errorf("页面导航能力未接入")
		}
		d.navigate(item.Ref)
		return nil
	case settings.TrayItemExe:
		return d.runExe(item)
	default:
		return fmt.Errorf("未知的条目类型: %q", item.Type)
	}
}

// runExe 启动任意外部程序。刻意不加 JobObject：用户主动拉起的桌面应用
// 生命周期独立于 Hanxi，退出 Hanxi 不应连带终止它们（与托盘语义一致）。
func (d *Dispatcher) runExe(item settings.TrayMenuItem) error {
	path := strings.TrimSpace(item.Path)
	if path == "" {
		return fmt.Errorf("未配置程序路径")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("程序不存在: %s", path)
	}

	cmd := exec.Command(path, SplitArgs(item.Args)...)
	if dir := filepath.Dir(path); dir != "" {
		cmd.Dir = dir
	}
	return cmd.Start()
}

// SplitArgs 将参数字符串拆为 argv：空白分隔，支持成对双引号包裹含空格的路径；
// 不识别转义符（Windows 启动参数场景够用，避免引入 shell 带来的注入风险）。
func SplitArgs(s string) []string {
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
