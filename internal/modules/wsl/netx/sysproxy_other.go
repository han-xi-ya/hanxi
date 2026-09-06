//go:build !windows

package netx

// systemProxyURL 非 Windows 平台无 WinINET 概念，恒为空（环境变量代理链路仍然生效）。
func systemProxyURL() string { return "" }
