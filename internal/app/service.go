package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"hubkit/internal/domain"
	"hubkit/internal/extapi"
	"hubkit/internal/settings"
)

// AppInfo 前端关于页/首页展示的应用信息。
type AppInfo struct {
	Name        string `json:"name"`
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
	registry *extapi.Registry
}

func NewAppService(registry *extapi.Registry) *AppService {
	return &AppService{registry: registry}
}

func (s *AppService) GetAppInfo() AppInfo {
	paths := settings.GetPaths()
	return AppInfo{
		Name:        Name,
		Version:     Version,
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
	// 确保目录存在
	_ = os.MkdirAll(targetPath, 0755)

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

// GetNavs 返回前端左侧导航（核心 + 已启用扩展）。
func (s *AppService) GetNavs() []extapi.NavEntry {
	return s.registry.GetEnabledNavs()
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
	info := s.registry.List()
	for i := range info {
		if info[i].ID == id {
			return &info[i], nil
		}
	}
	return nil, nil
}
