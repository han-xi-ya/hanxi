//go:build !windows

package supervisor

import "os/exec"

// applyWindowFlags 非 Windows 平台无"控制台窗口"语义，HideWindow 为 no-op；
// 保留本文件使包在 GOOS=linux 等平台可编译且对外 API 不变。
func applyWindowFlags(*exec.Cmd) {}
