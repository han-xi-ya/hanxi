package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hanxi/internal/platform/windows"
)

// 本文件收敛"打开系统面板/目录"类 RPC：项目为 Windows-only，
// 原四段同构 runtime.GOOS 分支按 Windows 口径直写，不再保留 darwin/linux 死分支。

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
	return exec.Command("explorer.exe", targetPath).Start()
}

// OpenHostsFile 使用系统默认记事本打开系统的 hosts 文件
func (s *AppService) OpenHostsFile() error {
	hostsPath := filepath.Join(windows.SystemRoot(), `System32\drivers\etc\hosts`)
	return exec.Command("notepad.exe", hostsPath).Start()
}

// OpenNetworkConnections 打开系统网络连接适配器控制面板 (ncpa.cpl)
func (s *AppService) OpenNetworkConnections() error {
	return exec.Command("control.exe", "ncpa.cpl").Start()
}

// OpenSystemEnvSettings 打开系统环境变量设置面板
func (s *AppService) OpenSystemEnvSettings() error {
	return exec.Command("rundll32.exe", "sysdm.cpl,EditEnvironmentVariables").Start()
}

// OpenSystemTool 按白名单调起 Windows 系统管理工具（设置页"系统快捷直达"）。
// 仅接受固定 key，杜绝任意命令注入；UAC 弹窗由系统自行处理（如注册表/计算机管理）。
func (s *AppService) OpenSystemTool(tool string) error {
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
