//go:build !windows

package version

// SystemInstall 非 Windows 平台恒为"未安装"（MSI 安装版形态是 Windows 专属，
// Hanxi 实际仅在 Windows 运行；本桩保编译，与模块内 *_other.go 同族）。
func DetectSystemInstall() (SystemInstall, bool) { return SystemInstall{}, false }
