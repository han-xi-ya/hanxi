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
	procIsWinVisible          = modUser32.NewProc("IsWindowVisible")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
)

const (
	wmClose = 0x0010

	// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
	// 直接失败码——未提权的 Hanxi 拉起 BCU 时得到它（不会代弹 UAC）。
	errorElevationRequired syscall.Errno = 740
)

// elevateHint 识别提权需求错误：BCU 的 app.manifest 为 requireAdministrator，
// 未提权父进程 CreateProcess 直接失败（740）且系统不代弹 UAC。
// 返回面向用户的指引文案；非该错误返回空串。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "BCU 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
	}
	return ""
}

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// 主窗口的 FormClosing 走正常关闭路径，SettingsProvider 落盘、互斥体释放、退出码 0。
// 窗口不存在（启动初期/已退出）静默返回。BCU 无固定窗口类名，必须按 PID 枚举。
func postCloseByPID(pid uint32) {
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var wpid uint32
		r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if r != 0 && wpid == uint32(lParam) {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // 继续枚举
	})
	_, _, _ = procEnumWindows.Call(cb, uintptr(pid))
}

// hasVisibleWindowByPID 指定进程是否存在可见顶层窗口（空闲退出豁免信号）。
func hasVisibleWindowByPID(pid uint32) bool {
	found := false
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var wpid uint32
		r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if r == 0 || wpid != uint32(lParam) {
			return 1
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible != 0 {
			found = true
			return 0 // 找到即可停止枚举
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, uintptr(pid))
	return found
}
