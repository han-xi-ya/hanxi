//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procPostMsg               = modUser32.NewProc("PostMessageW")
	procShowWindow            = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procIsWinVisible          = modUser32.NewProc("IsWindowVisible")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
)

const (
	wmClose = 0x0010

	// swRestore 从最小化/隐藏恢复窗口（ShowWindow 第 2 参）
	swRestore = 9
)

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// Flutter 默认关窗即进程退出；若上游改为驻托盘则宽限后强杀兜底。
// 窗口不存在（启动初期/已退出）静默返回。Flutter 窗口类名不可预测，按 PID 枚举。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口：可见窗口直接置前台；
// 最小化/隐藏窗口先 SW_RESTORE 再置前台（托盘驻留场）。
// FlClash 上游二次启动无唤窗行为，此路径是"打开窗口"的唯一实现。
func restoreWindowByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid != pid {
			return true
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			// 隐藏/最小化：先恢复再置前台（两调用有先后序）
			_, _, _ = procShowWindow.Call(hwnd, swRestore)
		}
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		return true
	})
}

// hasVisibleWindowByPIDs 这些进程中是否存在可见顶层窗口（空闲退出豁免信号）。
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
