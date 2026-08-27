//go:build !windows

package version

// DesktopRuntimeVersions 非 Windows 平台无 .NET 桌面运行时概念（HubKit 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒为空列表。
func DesktopRuntimeVersions() []string { return nil }

// desktopRuntimeVersionsUnder 非 Windows 平台 stub。
func desktopRuntimeVersionsUnder(string) []string { return nil }

// HasDesktopRuntimeMajor 非 Windows 平台 stub。
func HasDesktopRuntimeMajor([]string, string) bool { return false }
