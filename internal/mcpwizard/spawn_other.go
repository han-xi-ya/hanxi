//go:build !windows

package mcpwizard

import "syscall"

// hideChildWindow 非 Windows 平台无窗口面可藏（本应用 Windows-only，
// 此文件只为跨平台编译门存在，先例 card_other.go）。
func hideChildWindow() *syscall.SysProcAttr { return nil }
