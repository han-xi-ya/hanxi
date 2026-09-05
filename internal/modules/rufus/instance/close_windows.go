//go:build windows

package instance

import (
	"errors"
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

	// swRestore 从最小化恢复窗口（ShowWindow 第 2 参）
	swRestore = 9

	// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
	// 直接失败码——未提权的 Hanxi 拉起 Rufus 时得到它（不会代弹 UAC）。
	errorElevationRequired syscall.Errno = 740
)

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// Rufus 关主对话框即退出进程；若正有写盘任务在进行，上游会弹"任务进行中"
// 确认对话框挂住消息循环——宽限期内无人确认则落入强杀兜底（风险与
// 前端警示说明见包注释）。窗口不存在（已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口：可见窗口直接置前台，
// 最小化窗口先 SW_RESTORE 再置前台。Rufus 第二实例弹模态错误框退出、
// 无唤窗回调，此路径是"打开窗口"的唯一实现。
func restoreWindowByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid != pid {
			return true
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			// 最小化：先恢复再置前台（两调用有先后序）
			_, _, _ = procShowWindow.Call(hwnd, swRestore)
		}
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		return true
	})
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

// elevateHint 识别提权需求错误：Rufus manifest 为 requireAdministrator，
// 未提权父进程 CreateProcess 直接失败（740）且系统不代弹 UAC。
// 返回面向用户的指引文案；非该错误返回空串。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "Rufus 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
	}
	return ""
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
