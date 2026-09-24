//go:build !windows

package netx

// systemProxyURL 非 Windows 平台桩：无 WinINET 概念，恒空（链自然落到直连）。
// 交叉编译成对纪律同 supervisor/child_other.go 先例。
func systemProxyURL() string { return "" }
