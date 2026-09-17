package app

import (
	"runtime"
	"strings"

	"hanxi/internal/domain"
	"hanxi/internal/extapi"
	"hanxi/internal/product"
	"hanxi/internal/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 基础服务按职责拆分同包多文件（纯移动，签名零变化）：
//   - open_service.go     系统面板/目录打开类 RPC
//   - log_browse.go       日志浏览与清理
//   - settings_persist.go 常规设置/主题的读取持久化与回调注入
//   - elevate_tray.go     提权重启与托盘菜单配置
// 本文件仅保留应用信息、模块清单等装配核心。

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
	// windowDark 主窗口标题栏深色/色板应用回调（由装配根在窗口创建后注入，可能为 nil）。
	windowDark func(dark bool, accent string) error
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

// GetNavs 返回前端左侧导航（核心 + 已启用扩展）。
func (s *AppService) GetNavs() []extapi.NavEntry {
	return s.registry.GetEnabledNavs()
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
