//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procShowWindow            = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procIsWinVisible          = modUser32.NewProc("IsWindowVisible")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
)

// swRestore 恢复并显示窗口（含隐藏态——ShowWindow 对 SW_HIDE 窗口同样生效）
const swRestore = 9

// forEachWindow 统一封装 EnumWindows 枚举：回调返回 false 停止枚举。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var wpid uint32
		r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if r == 0 {
			return 1 // 拿不到 PID 的顶层窗（极罕见异常态）跳过
		}
		if visit(hwnd, wpid) {
			return 1
		}
		return 0
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
}

// hasVisibleWindowByPIDs 这些进程中是否存在可见顶层窗口。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	found := false
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if _, ok := set[wpid]; !ok {
			return true
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible != 0 {
			found = true
			return false // 找到即可停止枚举
		}
		return true
	})
	return found
}

// focusWindowsByPIDs 唤起这些进程的全部顶层窗口：最小化/隐藏先 SW_RESTORE
// 再置前台（与 litemonitor 同族方案，等价其托盘双击语义）。返回命中窗口数。
func focusWindowsByPIDs(set map[uint32]struct{}) int {
	count := 0
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if _, ok := set[wpid]; !ok {
			return true
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			_, _, _ = procShowWindow.Call(hwnd, swRestore)
		}
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		count++
		return true // 继续枚举其余窗口
	})
	return count
}
