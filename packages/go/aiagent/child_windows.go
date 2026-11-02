//go:build windows

package aiagent

import (
	"os/exec"
	"syscall"
)

// hideChildConsole 让子进程不弹控制台黑框。Hanxi 本体是**无控制台的 GUI 程序**
// （-H windowsgui），而本机 `claude` 经 npm 分发、LookPath 命中的是 claude.cmd：
// 不设这两项，按下「让 AI 提取」后用户看到的第一个反馈是一个黑框立起来几十秒
// （实测一次往返 66s），界面像卡死而不是在等——这跟超时预算是同一个用户体验的两半。
//
// CREATE_NO_WINDOW 不为子进程分配新控制台，HideWindow 让窗口保持隐藏
// （与 packages/go/supervisor/child_windows.go、internal/modules/frpc|ocr 的
// instance/child_windows.go 蓝本同口径）。
func hideChildConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
