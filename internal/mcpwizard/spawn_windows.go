//go:build windows

package mcpwizard

import "syscall"

// hideChildWindow 自检子进程绝不带出控制台窗口：生产 exe 本身是 -H windowsgui
// （#63 实测其 stdio 管道继承正常），开发/测试形态的控制台构建则靠 HideWindow
// 免黑框。
func hideChildWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
