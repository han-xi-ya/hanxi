// Package launcher 统一执行 TrayMenuItem 配置条目：托盘右键菜单与鼠标唤出快捷菜单
// （quickmenu）共享同一套条目语义（显示名解析 + command/route/exe 三类动作分发、
// 停用模块可见性收口），避免两套菜单各写一份分发逻辑而漂移；但两家的账本各读各的
// （EnabledItems→TrayMenu / WheelItems→WheelMenu，机主拍板 2026-09-26 互不干扰）。
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

// EnabledItems 返回托盘账（TrayMenu）已启用条目的副本（配置保存顺序即展示顺序）。
// 轮盘弹出清单不走本方法——见 WheelItems。
func (d *Dispatcher) EnabledItems() []settings.TrayMenuItem {
	if d.store == nil {
		return nil
	}
	return enabledItems(d.store.GetTrayMenu())
}

// WheelItems 返回轮盘独立账（WheelMenu）已启用条目的副本（保存顺序即盘面扇区序）。
// 与 EnabledItems 同语义不同账源：托盘账怎么改都不影响本方法读数，反之亦然。
func (d *Dispatcher) WheelItems() []settings.TrayMenuItem {
	if d.store == nil {
		return nil
	}
	return enabledItems(d.store.GetWheelMenu())
}

// enabledItems 过滤顶层启用条目（两账共用一套过滤语义）。group 子条目的
// Enabled 过滤留给消费方（托盘 build 展子菜单、轮盘 wheelView 拍平/子盘），与历史行为一致。
func enabledItems(items []settings.TrayMenuItem) []settings.TrayMenuItem {
	var out []settings.TrayMenuItem
	for _, item := range items {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out
}

// ItemModuleID 解析条目引用的模块 ID：command 取 Ref 的 key 前缀
// （"moduleId/commandId"），route 取 "/ext/<id>" 前缀下的首段；exe、核心
// 路由（如 /settings）与其他非模块引用返回空串。
func ItemModuleID(item settings.TrayMenuItem) string {
	switch item.Type {
	case settings.TrayItemCommand:
		if i := strings.IndexByte(item.Ref, '/'); i > 0 {
			return item.Ref[:i]
		}
	case settings.TrayItemRoute:
		const prefix = "/ext/"
		if rest := strings.TrimPrefix(item.Ref, prefix); rest != item.Ref {
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				rest = rest[:i]
			}
			return rest
		}
	}
	return ""
}

// FilterDisabledModuleItems 滤除引用了停用模块的条目（含 group 子条目，递归）。
// 仅当 moduleId 存在于 state（注册表已知）且为 false 时剔除；未知 ID（历史残留
// 配置、非模块体系条目）原样保留，与 launcher 的"配置为准"语义一致。
// 纯函数形态（启停表显式传入）便于表测；托盘与轮盘两径共用本实现（Wave 1 可见性收口）。
func FilterDisabledModuleItems(items []settings.TrayMenuItem, state map[string]bool) []settings.TrayMenuItem {
	if len(items) == 0 {
		return items
	}
	out := make([]settings.TrayMenuItem, 0, len(items))
	for _, item := range items {
		if id := ItemModuleID(item); id != "" {
			if enabled, known := state[id]; known && !enabled {
				continue
			}
		}
		if item.Type == settings.TrayItemGroup {
			item.Children = FilterDisabledModuleItems(item.Children, state)
		}
		out = append(out, item)
	}
	return out
}

// FilterDisabledModules 按注册表当前模块启停表现过滤条目（registry 为 nil 或
// 查不到模块时不过滤，语义同上）。调用方现读现滤，无需自行快照启停表。
func (d *Dispatcher) FilterDisabledModules(items []settings.TrayMenuItem) []settings.TrayMenuItem {
	state := make(map[string]bool)
	if d.registry != nil {
		for _, info := range d.registry.List() {
			state[info.ID] = info.Enabled
		}
	}
	return FilterDisabledModuleItems(items, state)
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
	case settings.TrayItemGroup:
		return "分组"
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
	case settings.TrayItemGroup:
		// group 是容器不是动作：轮盘展开子盘、托盘展开子菜单，均不走到执行路径。
		return fmt.Errorf("分组条目不可直接执行")
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
