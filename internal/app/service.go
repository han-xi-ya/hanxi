package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	for i := 0; i < len(list)-1; i++ {
		for j := i + 1; j < len(list); j++ {
			if list[i].ModTime < list[j].ModTime {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

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
