//go:build !windows

package search

import "os/exec"

// hideConsole 非 Windows 平台无控制台窗口概念（保持跨平台可编译）。
func hideConsole(*exec.Cmd) {}
