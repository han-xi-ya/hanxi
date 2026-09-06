//go:build !windows

package version

// DetectInstalled 非 Windows 平台占位：安装版感知仅存在于 Windows。
func DetectInstalled() InstalledInfo {
	return InstalledInfo{}
}
