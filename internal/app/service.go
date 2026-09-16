package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"hanxi/internal/domain"
	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppInfo 前端关于页/首页展示的应用信息。
type AppInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tagline     string `json:"tagline"`
	Version     string `json:"version"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	Mode        string `json:"mode"`
	BaseDir     string `json:"baseDir"`
	ConfigDir   string `json:"configDir"`
	LogsDir     string `json:"logsDir"`
	VersionsDir string `json:"versionsDir"`
	RuntimeDir  string `json:"runtimeDir"`
}

// AppService 是暴露给前端的基础服务：
// 应用信息、模块清单与导航（扩展注入的入口）。
type AppService struct {
	registry    *extapi.Registry
	store       *settings.Store
	trayRebuild func() // 托盘菜单热重建回调（由装配根注入，可能为 nil）
	// windowDark 主窗口标题栏深色应用回调（由装配根在窗口创建后注入，可能为 nil）。
	windowDark func(dark bool) error
}

// NewAppService 创建基础服务。trayRebuild / windowDark 回调此时为 nil，
// 由装配根在托盘/主窗口创建后经 SetTrayRebuilder / SetWindowDarkApplier 注入。
func NewAppService(registry *extapi.Registry, store *settings.Store) *AppService {
	return &AppService{registry: registry, store: store}
}

// GetAppInfo 返回产品标识、运行模式与全套数据目录路径，供前端关于页/首页展示。
// 依赖 InitPaths 已执行（GetPaths 内部兜底懒初始化）。
func (s *AppService) GetAppInfo() AppInfo {
	paths := settings.GetPaths()
	return AppInfo{
		Name:        product.Name,
		Description: product.Description,
		Tagline:     product.Tagline,
		Version:     product.Version,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Mode:        string(paths.Mode()),
		BaseDir:     paths.BaseDir(),
		ConfigDir:   paths.ConfigDir(),
		LogsDir:     paths.LogsDir(),
		VersionsDir: paths.VersionsDir(),
		RuntimeDir:  paths.RuntimeDir(),
	}
}

// OpenPath 在系统资源管理器中打开指定目录或选中文件
func (s *AppService) OpenPath(targetPath string) error {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return fmt.Errorf("路径不能为空")
	}
	// 若路径不存在，尝试创建（目录场景）
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.MkdirAll(targetPath, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("explorer.exe", targetPath)
	} else if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", targetPath)
	} else {
		cmd = exec.Command("xdg-open", targetPath)
	}
	return cmd.Start()
}

// OpenHostsFile 使用系统默认记事本或编辑器打开系统的 hosts 文件
func (s *AppService) OpenHostsFile() error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		hostsPath := fmt.Sprintf(`%s\System32\drivers\etc\hosts`, systemRoot)
		cmd = exec.Command("notepad.exe", hostsPath)
	} else if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-e", "/etc/hosts")
	} else {
		cmd = exec.Command("xdg-open", "/etc/hosts")
	}
	return cmd.Start()
}

// OpenNetworkConnections 打开系统网络连接适配器控制面板 (ncpa.cpl)
func (s *AppService) OpenNetworkConnections() error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("control.exe", "ncpa.cpl")
	} else if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "/System/Library/PreferencePanes/Network.prefPane")
	} else {
		cmd = exec.Command("nm-connection-editor")
	}
	return cmd.Start()
}

// OpenSystemEnvSettings 打开系统环境变量设置面板
func (s *AppService) OpenSystemEnvSettings() error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("rundll32.exe", "sysdm.cpl,EditEnvironmentVariables")
	} else {
		return fmt.Errorf("当前系统不支持快捷打开环境变量")
	}
	return cmd.Start()
}

// OpenSystemTool 按白名单调起 Windows 系统管理工具（设置页"系统快捷直达"）。
// 仅接受固定 key，杜绝任意命令注入；UAC 弹窗由系统自行处理（如注册表/计算机管理）。
func (s *AppService) OpenSystemTool(tool string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前系统不支持快捷直达")
	}
	var cmd *exec.Cmd
	switch tool {
	case "control": // 控制面板主页
		cmd = exec.Command("control.exe")
	case "regedit": // 注册表编辑器
		cmd = exec.Command("regedit.exe")
	case "firewall": // Windows 防火墙
		cmd = exec.Command("control.exe", "firewall.cpl")
	case "compmgmt": // 计算机管理（mmc 加载，兼容非 System32 工作目录）
		cmd = exec.Command("mmc.exe", "compmgmt.msc")
	default:
		return fmt.Errorf("未知的系统工具: %s", tool)
	}
	return cmd.Start()
}

// GetNavs 返回前端左侧导航（核心 + 已启用扩展）。
func (s *AppService) GetNavs() []extapi.NavEntry {
	return s.registry.GetEnabledNavs()
}

// LogFileInfo 日志文件基本元数据
type LogFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// ListLogFiles 获取日志目录下的所有日志文件列表（按时间倒序排列）
func (s *AppService) ListLogFiles() ([]LogFileInfo, error) {
	logsDir := settings.GetPaths().LogsDir()
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LogFileInfo{}, nil
		}
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	var list []LogFileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		list = append(list, LogFileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	// 按修改时间倒序
	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime > list[j].ModTime
	})

	return list, nil
}

// ReadLogContent 读取指定日志文件的内容（限制最大行数避免内存溢出，默认倒序取最新行）
func (s *AppService) ReadLogContent(fileName string, maxLines int) (string, error) {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." || fileName == "/" || fileName == "\\" {
		return "", fmt.Errorf("无效的日志文件名")
	}

	logsDir := settings.GetPaths().LogsDir()
	targetPath := filepath.Join(logsDir, fileName)

	contentBytes, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("读取日志文件失败: %w", err)
	}

	raw := string(contentBytes)
	if maxLines <= 0 {
		maxLines = 500
	}

	lines := strings.Split(raw, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n"), nil
}

// ClearLogs 清除所有历史日志（保留当天的）
func (s *AppService) ClearLogs() error {
	logsDir := settings.GetPaths().LogsDir()
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	todayLog := "app-" + today + ".log"

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") && entry.Name() != todayLog {
			_ = os.Remove(filepath.Join(logsDir, entry.Name()))
		}
	}
	return nil
}

// EnsureModuleActive 确保指定模块已按需完成懒初始化（在进入模块路由时调用）
func (s *AppService) EnsureModuleActive(moduleID string) error {
	return s.registry.EnsureActive(strings.TrimSpace(moduleID))
}

// ListModules 返回模块清单与启用状态（设置页）。
func (s *AppService) ListModules() []extapi.ModuleInfo {
	return s.registry.List()
}

// SetModuleEnabled 设置页开关模块；启用/禁用成功返回最新元信息。
func (s *AppService) SetModuleEnabled(id string, enabled bool) (*extapi.ModuleInfo, error) {
	if err := s.registry.SetEnabled(id, enabled); err != nil {
		ae := domain.NewAppError(domain.ErrValidation, "无效的扩展 ID")
		ae.Cause = err
		return nil, ae
	}

	// 广播扩展与导航变化事件，通知前端实时热更新侧边栏与页面
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ext:changed")
	}

	info := s.registry.List()
	for i := range info {
		if info[i].ID == id {
			return &info[i], nil
		}
	}
	return nil, nil
}

// GeneralSettings 前端通用设置模型
type GeneralSettings struct {
	AutoStart      bool `json:"autoStart"`
	MinimizeToTray bool `json:"minimizeToTray"`
	LogRetainDays  int  `json:"logRetainDays"`
}

// GetGeneralSettings 获取常规配置（开机自启、最小化托盘等）
func (s *AppService) GetGeneralSettings() GeneralSettings {
	if s.store == nil {
		return GeneralSettings{
			AutoStart:      false,
			MinimizeToTray: true,
			LogRetainDays:  7,
		}
	}
	cfg := s.store.Get()
	// 如果在 Windows 平台，以注册表的实际状态同步
	if runtime.GOOS == "windows" {
		cfg.AutoStart = windows.IsAutoStart()
	}
	return GeneralSettings{
		AutoStart:      cfg.AutoStart,
		MinimizeToTray: cfg.MinimizeToTray,
		LogRetainDays:  cfg.LogRetainDays,
	}
}

// SetGeneralSettings 保存常规配置
func (s *AppService) SetGeneralSettings(gen GeneralSettings) error {
	if runtime.GOOS == "windows" {
		if err := windows.SetAutoStart(gen.AutoStart); err != nil {
			return fmt.Errorf("设置开机自启动失败: %w", err)
		}
	}

	if s.store != nil {
		return s.store.Update(func(cfg *settings.AppSettings) {
			cfg.AutoStart = gen.AutoStart
			cfg.MinimizeToTray = gen.MinimizeToTray
			if gen.LogRetainDays > 0 {
				cfg.LogRetainDays = gen.LogRetainDays
			}
		})
	}
	return nil
}

// SetTrayRebuilder 注入托盘菜单热重建回调，仅由装配根（app.New）在托盘创建后调用。
func (s *AppService) SetTrayRebuilder(fn func()) { s.trayRebuild = fn }

// SetWindowDarkApplier 注入主窗口标题栏深色应用回调，仅由装配根（app.New）在窗口创建后调用。
func (s *AppService) SetWindowDarkApplier(fn func(dark bool) error) { s.windowDark = fn }

// GetTheme 返回持久化的主题模式："light" | "dark" | "system"（异常/未设置回退浅色）。
func (s *AppService) GetTheme() string {
	if s.store == nil {
		return "light"
	}
	switch t := s.store.Get().Theme; t {
	case "light", "dark", "system":
		return t
	default:
		return "light"
	}
}

// SetTheme 持久化主题模式。DOM 与标题栏的实际应用由前端 useTheme 完成
// （其解析 system→亮/暗后回调 SetWindowDarkMode），后端不感知解析细节。
func (s *AppService) SetTheme(mode string) error {
	switch mode {
	case "light", "dark", "system":
	default:
		return fmt.Errorf("非法主题模式: %s", mode)
	}
	if s.store == nil {
		return nil
	}
	return s.store.Update(func(cfg *settings.AppSettings) {
		cfg.Theme = mode
	})
}

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
// 「以管理员身份重启」入口的显隐）。非 Windows 无 UAC 语义，恒返回 true
// 让前端不显示按钮。
func (s *AppService) IsElevated() bool {
	if runtime.GOOS != "windows" {
		return true
	}
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
	if runtime.GOOS != "windows" {
		return fmt.Errorf("提权重启仅支持 Windows")
	}
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
			time.Sleep(300 * time.Millisecond)
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
