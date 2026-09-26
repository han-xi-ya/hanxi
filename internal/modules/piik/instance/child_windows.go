//go:build windows

package instance

import (
	"os/exec"
	"syscall"
)

// createNoWindow 控制台子系统的创建标志：CREATE_NO_WINDOW 不为子进程分配
// 新控制台窗口；HideWindow 让窗口保持隐藏。Hanxi 本体是无控制台的 GUI 程序，
// piik-app.exe 若不加此标志拉起会闪黑窗（frpc 同款问题与同款解法）。
const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// applyWindowFlags 按规格无窗化子进程创建（主流程零平台分支，差异收口在
// 本文件与 child_other.go——supervisor 内核 child 文件同款形制）。
func applyWindowFlags(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
