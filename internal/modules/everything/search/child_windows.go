//go:build windows

package search

import (
	"os/exec"
	"syscall"
)

// hideConsole 防止控制台子系统子进程（ES.exe 是 console 程序）从 GUI 父进程
// 拉起时闪出一个黑色控制台窗口。GUI 子系统程序不受影响，无需此设置。
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}