//go:build !windows

package instance

import "os/exec"

// hideWindow 非 Windows 平台无窗口抑制需求（no-op）。
func hideWindow(cmd *exec.Cmd) {}
