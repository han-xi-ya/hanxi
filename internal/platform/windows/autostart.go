//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = `HubKit`

// SetAutoStart 设置或移除 Windows 当前用户的开机自启动注册表项
func SetAutoStart(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open registry Run key failed: %w", err)
	}
	defer k.Close()

	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path failed: %w", err)
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return fmt.Errorf("get absolute path failed: %w", err)
		}

		// 启动参数添加 --minimized，支持开机静默常驻托盘
		cmd := fmt.Sprintf("\"%s\" --minimized", exePath)
		if err := k.SetStringValue(runValueName, cmd); err != nil {
			return fmt.Errorf("write registry Run value failed: %w", err)
		}
		return nil
	}

	// 禁用自启：删除键值（若不存在则忽略错误）
	err = k.DeleteValue(runValueName)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("delete registry Run value failed: %w", err)
	}
	return nil
}

// IsAutoStart 检查当前程序是否已注册在开机自启动项中
func IsAutoStart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}

	exePath, err := os.Executable()
	if err != nil {
		return len(val) > 0
	}
	exePath, _ = filepath.Abs(exePath)

	return strings.Contains(strings.ToLower(val), strings.ToLower(filepath.Base(exePath)))
}
