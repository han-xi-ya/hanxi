//go:build windows

package instance

import (
	"os/exec"
	"syscall"
)

// createNoWindow 控制台子系统子进程（hanxi-ocr.exe）的创建标志：
// CREATE_NO_WINDOW 不为子进程分配新控制台窗口。Hanxi 本体是无控制台的
// GUI 程序，若不设置，拉起 hanxi-ocr.exe 会弹出黑窗口。
const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// hideWindow 配置子进程不弹控制台窗口。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
