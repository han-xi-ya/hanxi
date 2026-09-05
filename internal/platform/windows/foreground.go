//go:build windows

package windows

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	modUser32Fg = syscall.NewLazyDLL("user32.dll")

	procGetForegroundWindow = modUser32Fg.NewProc("GetForegroundWindow")
	procGetWindowThreadID   = modUser32Fg.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput   = modUser32Fg.NewProc("AttachThreadInput")
	procSetForeground       = modUser32Fg.NewProc("SetForegroundWindow")
	procBringToTop          = modUser32Fg.NewProc("BringWindowToTop")
)

// SetForegroundForce 将指定窗口强制置于前台并抢焦点。
//
// 普通 SetForegroundWindow 在调用进程处于后台时（典型如主窗隐藏于托盘、用户刚在
// 别的应用里操作）会被系统前台锁（ForegroundLockTimeout）拒绝——表现为弹窗画出
// 来了但拿不到焦点，键盘 Esc 与失焦收起全部失灵。经 AttachThreadInput 把本线程与
// 当前前台窗口线程的输入队列互相绑定后，即可借用那次用户输入的"前台特权"完成
// 切换，是 Win32 时代的标准规避手法（用完必须解绑，避免输入队列长期共享）。
func SetForegroundForce(hwnd uintptr) error {
	fg, _, _ := procGetForegroundWindow.Call()
	fgTid, _, _ := procGetWindowThreadID.Call(fg, 0)
	curTid := uintptr(windows.GetCurrentThreadId())

	attached := false
	if fgTid != 0 && fgTid != curTid {
		r, _, _ := procAttachThreadInput.Call(curTid, fgTid, 1)
		attached = r != 0
	}

	set, _, err := procSetForeground.Call(hwnd)
	procBringToTop.Call(hwnd)

	if attached {
		procAttachThreadInput.Call(curTid, fgTid, 0)
	}
	if set == 0 {
		return fmt.Errorf("SetForegroundWindow 失败: %w", err)
	}
	return nil
}
