//go:build !windows

package version

import "fmt"

// runInstallerSilent 非 Windows 平台无 NSIS 语义（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：直接报错。
func runInstallerSilent(installer, targetDir string) error {
	return fmt.Errorf("Recordly 托管安装仅支持 Windows")
}

func foreignInstallLocation(versionsDir string) (string, bool) { return "", false }

func cleanupShortcuts(versionsDir, desktopDir string) {}
