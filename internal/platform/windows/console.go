//go:build windows

package windows

import (
	"os/exec"
	"syscall"
)

// createNoWindow CREATE_NO_WINDOW：控制台子系统程序隐藏启动黑框。
const createNoWindow = 0x08000000

// HideConsole 配置 cmd 以隐藏控制台窗口方式启动（一次性探测/后台命令统一走它）。
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
