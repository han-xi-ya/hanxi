package app

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// elevatedRestartQuitDelay 提权重启后延迟退出的时长：让 RestartElevated RPC 的
// 响应先发回前端，本实例再启动退出流程。
const elevatedRestartQuitDelay = 300 * time.Millisecond

// restartRouteRe 交接路由门卫：前端路由形如 /ext/<id> 或 /settings，
// 只放行小写字母/数字/短横/斜杠路径；非法值静默丢弃（退回首页无害）。
var restartRouteRe = regexp.MustCompile(`^/[a-z0-9][a-z0-9/-]{0,63}$`)

// SanitizeRoute 校验并返回合法交接路由，非法返回空串。
// 双消费方：cmd 入口过滤 -route 启动参数；RestartElevated RPC 过滤前端传参。
func SanitizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if !restartRouteRe.MatchString(route) {
		return ""
	}
	return route
}

// IsElevated 报告当前进程是否已以管理员提权运行（前端据此决定
// 「以管理员身份重启」入口的显隐）。
func (s *AppService) IsElevated() bool {
	return windows.IsElevated()
}

// RestartElevated 经一次 UAC 以管理员身份重启 Hanxi（requireAdministrator
// 托管模块 BCU/Rufus/LiteMonitor 的 740 直拒解药，见 elevateHint 文案与
// TROUBLESHOOTING #17）。route 为重启后前端应直达的路由，传空则默认首页。
//
// 时序：UAC 用户点"是"（新实例已创建）→ 本 RPC 先返回 → 稍后走正常退出
// 流程（OnShutdown 按各模块"随 Hanxi 一起关闭"开关收尾，与手动退出口径一致
// ——默认 Detached 的工具跨提权重启继续存活）。用户点"否"
// 则返回取消错误，本实例原地不动。新实例经 -takeover 等待本进程退出后才
// 抢单实例锁，不会互撞。
func (s *AppService) RestartElevated(route string) error {
	if windows.IsElevated() {
		return fmt.Errorf("Hanxi 当前已是管理员权限运行，无需重启")
	}
	args := []string{fmt.Sprintf("-takeover=%d", os.Getpid())}
	if r := SanitizeRoute(route); r != "" {
		args = append(args, "-route="+r)
	}
	if err := windows.RestartElevated(args); err != nil {
		return err
	}
	// 延后一拍再退：让本 RPC 的响应先发回前端，再启动退出。
	if a := application.Get(); a != nil {
		go func() {
			time.Sleep(elevatedRestartQuitDelay)
			a.Quit()
		}()
	}
	return nil
}

// SetWindowDarkMode 切换主窗口原生标题栏亮/暗（DWM ImmersiveDarkMode）。
func (s *AppService) SetWindowDarkMode(dark bool) error {
	if s.windowDark == nil {
		return nil // 窗口未就绪（启动早期/装配缺失）时静默降级
	}
	return s.windowDark(dark)
}

// TrayMenuOption 设置页可选的托盘菜单候选项（托管命令与扩展页面导航）。
// 自定义外部程序不属候选目录，由前端构造 type=exe 条目随 SetTrayMenu 提交。
type TrayMenuOption struct {
	Type       string `json:"type"`       // "command" | "route"
	Ref        string `json:"ref"`        // command: "moduleId/commandId"；route: 前端路由
	Label      string `json:"label"`      // 默认显示名
	ModuleName string `json:"moduleName"` // 所属模块（页面导航为空）
}

// ListTrayMenuOptions 返回全部可启用的托盘菜单候选项（仅收集已启用模块）。
func (s *AppService) ListTrayMenuOptions() []TrayMenuOption {
	out := []TrayMenuOption{}
	for _, cmd := range s.registry.ListTrayCommands() {
		out = append(out, TrayMenuOption{Type: settings.TrayItemCommand, Ref: cmd.Key, Label: cmd.Label, ModuleName: cmd.ModuleName})
	}
	for _, nav := range s.registry.GetEnabledNavs() {
		if nav.Section != extapi.SectionExt {
			continue
		}
		out = append(out, TrayMenuOption{Type: settings.TrayItemRoute, Ref: nav.Route, Label: nav.Title})
	}
	return out
}

// GetTrayMenu 返回当前托盘右键菜单配置条目（按保存顺序）。
func (s *AppService) GetTrayMenu() []settings.TrayMenuItem {
	if s.store == nil {
		return []settings.TrayMenuItem{}
	}
	return s.store.GetTrayMenu()
}

// cleanTrayLeaf 校验并规整一个叶子条目（command/route/exe）：字段去空白、必填检查。
// group 型不允许进叶子位（嵌套深度锁死两层，二级盘不需要无限树）。
func cleanTrayLeaf(it settings.TrayMenuItem) (settings.TrayMenuItem, error) {
	it.Type = strings.TrimSpace(it.Type)
	it.Ref = strings.TrimSpace(it.Ref)
	it.Path = strings.TrimSpace(it.Path)
	it.Children = nil
	switch it.Type {
	case settings.TrayItemCommand, settings.TrayItemRoute:
		if it.Ref == "" {
			return it, fmt.Errorf("托盘条目缺少引用（%s）", it.Type)
		}
	case settings.TrayItemExe:
		if it.Path == "" {
			return it, fmt.Errorf("外部程序托盘条目缺少路径")
		}
	case settings.TrayItemGroup:
		return it, fmt.Errorf("分组条目不允许再嵌套分组")
	default:
		return it, fmt.Errorf("未知托盘条目类型: %q", it.Type)
	}
	return it, nil
}

// SetTrayMenu 校验并持久化托盘菜单配置，保存成功后立即重建右键菜单热生效。
// group 条目：必须有名字与至少一个子条目，子条目经同一叶子校验规整。
func (s *AppService) SetTrayMenu(items []settings.TrayMenuItem) error {
	cleaned := make([]settings.TrayMenuItem, 0, len(items))
	for _, it := range items {
		it.Type = strings.TrimSpace(it.Type)
		if it.Type == settings.TrayItemGroup {
			it.Label = strings.TrimSpace(it.Label)
			if it.Label == "" {
				return fmt.Errorf("分组条目缺少名称")
			}
			if len(it.Children) == 0 {
				return fmt.Errorf("分组「%s」没有任何子条目", it.Label)
			}
			it.Ref, it.Path, it.Args = "", "", ""
			kids := make([]settings.TrayMenuItem, 0, len(it.Children))
			for _, ch := range it.Children {
				kid, err := cleanTrayLeaf(ch)
				if err != nil {
					return fmt.Errorf("分组「%s」：%w", it.Label, err)
				}
				kids = append(kids, kid)
			}
			it.Children = kids
		} else {
			leaf, err := cleanTrayLeaf(it)
			if err != nil {
				return err
			}
			it = leaf
		}
		cleaned = append(cleaned, it)
	}
	if s.store == nil {
		return fmt.Errorf("配置存储不可用")
	}
	if err := s.store.SetTrayMenu(cleaned); err != nil {
		return err
	}
	if s.trayRebuild != nil {
		s.trayRebuild()
	}
	return nil
}

// PickExeFile 弹出系统文件选择框选取外部程序，返回绝对路径（用户取消时为空串）。
func (s *AppService) PickExeFile() (string, error) {
	a := application.Get()
	if a == nil || a.Dialog == nil {
		return "", fmt.Errorf("对话框服务不可用")
	}
	path, err := a.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("程序与快捷方式", "*.exe;*.bat;*.cmd;*.lnk").
		SetTitle("选择要启动的程序").
		PromptForSingleSelection()
	if err != nil {
		// Wails 将用户取消也作为 error 返回（cfd.ErrorCancelled = "cancelled by user"），
		// 但 cfd 位于 wails 的 internal 包无法导入做哨兵比较，按文案归一化为"取消 → 空串"。
		if strings.Contains(strings.ToLower(err.Error()), "cancel") {
			return "", nil
		}
		return "", err
	}
	return path, nil
}
