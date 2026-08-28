//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	closeUser32                  = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = closeUser32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = closeUser32.NewProc("GetWindowThreadProcessId")
	procPostMessage              = closeUser32.NewProc("PostMessageW")
)

const wmClose = 0x0010

// postCloseByPID 向指定 MangoDisk 进程的全部顶层窗口投递 WM_CLOSE。
// 单实例 signal window 只用于确定 PID，不把它误当作主窗口。
func postCloseByPID(pid uint32) {
	if pid == 0 {
		return
	}
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var windowPID uint32
		ret, _, _ := procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		if ret != 0 && windowPID == uint32(lParam) {
			_, _, _ = procPostMessage.Call(hwnd, wmClose, 0, 0)
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, uintptr(pid))
}
