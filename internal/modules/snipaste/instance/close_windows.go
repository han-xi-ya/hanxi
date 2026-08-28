//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procPostMessageW          = modUser32.NewProc("PostMessageW")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWindowThreadProcID = modUser32.NewProc("GetWindowThreadProcessId")
)

const wmClose = 0x0010

// postCloseByPID 向自有 PID 的全部顶层窗口投递 WM_CLOSE。
// Snipaste 是闭源 Qt 托盘程序，此操作仅是尽力关闭请求，不等价于已证实的退出协议。
func postCloseByPID(pid uint32) int {
	count := 0
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var windowPID uint32
		r, _, _ := procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		if r != 0 && windowPID == uint32(lParam) {
			if posted, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0); posted != 0 {
				count++
			}
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, uintptr(pid))
	return count
}
