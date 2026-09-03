//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procIsWinVisible          = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen      = modUser32.NewProc("GetWindowTextLengthW")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
	procPostMsg               = modUser32.NewProc("PostMessageW")
)

const wmClose = 0x0010

// postClose 向所有归属 Recordly.exe 进程、可见且带标题的顶层窗口投递 WM_CLOSE。
// Electron 窗口类名是 Chromium 通用的 Chrome_WidgetWin_1（与一切 Chromium
// 应用共享），绝不能按类名 FindWindow——会误伤用户浏览器窗口，必须
// EnumWindows + 进程名过滤（bcu 同款思路，按名而非按 PID 以便覆盖外部实例）。
//
// 上游语义（main.ts 实证）：Windows 无托盘（shouldUseTray 仅 Linux），
// 主窗口关闭 → window-all-closed → app.quit()，优雅退出可达；
// 编辑器窗口关闭 = 收起回 HUD（不退出应用），多窗口场由 Quit 的
// 宽限 + JobObject 强杀兜底收敛。无窗口（启动初期/已退出）静默返回。
func postClose() {
	pids := recordlyPIDs()
	if len(pids) == 0 {
		return
	}
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return 1
		}
		var wpid uint32
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && pids[wpid] {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // 继续枚举：多窗口场全部送达
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
}
