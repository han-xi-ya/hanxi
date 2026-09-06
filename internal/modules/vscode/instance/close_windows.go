//go:build windows

package instance

import (
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

const wmClose = 0x0010

// postCloseByPID 向指定进程的所有可见顶层窗口投递 WM_CLOSE：
// VS Code（Electron）关最后一个窗口即退出主进程（未保存内容有 hot exit
// 备份兜底）；宽限期后仍有窗口未响应（模态对话框等）由 JobObject 强杀兜底。
// 窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			if visible, _, _ := procIsWinVisible.Call(hwnd); visible != 0 {
				_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
			}
		}
		return true // 继续枚举（同 PID 可能多窗口：编辑器主窗 + 浮动面板窗）
	})
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
