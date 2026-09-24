//go:build windows

package instance

import (
	"errors"
	"syscall"
	"unsafe"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procPostMsg               = modUser32.NewProc("PostMessageW")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
)

const (
	wmClose = 0x0010

	// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
	// 直接失败码——未提权的 Hanxi 拉起 LiteMonitor 时得到它（不会代弹 UAC）。
	errorElevationRequired syscall.Errno = 740
)

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// LiteMonitor 退出菜单即 form.Close() 且无 FormClosing 拦截——关窗 = 退进程；
// 若窗口被 HideMainForm 隐藏，WM_CLOSE 对隐藏窗仍有效。
// 窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口（N3 收口：委托平台公共件）。
// LiteMonitor 的第二实例静默退出无唤窗回调，此路径是"打开窗口"的唯一实现。
// 旧实现以 "IsWindowVisible==0" 判最小化——最小化窗 WS_VISIBLE 恒真，恢复支
// 永不触发且裸置前台遭前台锁拒（托盘后台态"点了没反应"病灶），公共件按
// 可见+标题过滤、IsIconic 恢复、借权置前三要素收口。
func restoreWindowByPID(pid uint32) {
	win.FocusTopWindowForPID(pid)
}

// hasVisibleWindowByPIDs 这些进程中是否存在可唤顶层窗口（与唤窗同一判据，
// 含最小化在场；隐藏辅助窗不计数——service 空闲豁免/唤窗分流共用一判）。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	return win.HasFocusableTopWindowForPIDs(set)
}

// elevateHint 识别提权需求错误：LiteMonitor manifest 为 requireAdministrator，
// 未提权父进程 CreateProcess 直接失败（740）且系统不代弹 UAC。
// 返回面向用户的指引文案；非该错误返回空串。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "LiteMonitor 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
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
