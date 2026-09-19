//go:build windows

package supervisor

import (
	"os/exec"
	"syscall"
)

// createNoWindow 控制台子系统子进程的创建标志：CREATE_NO_WINDOW 不为子进程分配
// 新控制台窗口；HideWindow 让窗口保持隐藏。Hanxi 本体是无控制台的 GUI 程序，
// 若不设置，拉起控制台工具会弹出黑窗口（与 internal/modules/frpc|ddnsgo|ocr 的
// child_windows.go 蓝本同口径）。
const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// applyWindowFlags 配置子进程不弹控制台窗口（仅在 Spec.HideWindow=true 时被调用）。
func applyWindowFlags(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
