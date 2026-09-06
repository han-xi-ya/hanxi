//go:build !windows

package version

import "fmt"

// runInstallerSilent 非 Windows 平台占位：VS Code 安装版托管仅存在于 Windows。
func runInstallerSilent(installer, version string) error {
	return fmt.Errorf("安装版静默安装仅在 Windows 可用")
}
