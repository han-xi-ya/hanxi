//go:build windows

package aiagent

import (
	"os/exec"
	"strconv"
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

// killProcessTree 收掉 .cmd → cmd.exe → node 整棵树。
// 真机实测（2026-10-30 stream 探测 E4）：Process.Kill 只杀得着头（cmd.exe），
// node 孙进程存活继续攥着会话；taskkill /T /F 对同一次实验的三个 PID 全收。
// 常驻会话的 Close 必须走这条，否则"关聊天窗"关不掉后台烧网络的 node。
// 走 taskkill 命令行而不是 JobObject：那是托管进程线的内核，不该由文本会话引进
// （与 agent.go 头注对 JobObject 的取舍同一口径）。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	tk := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	tk.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return tk.Run()
}
