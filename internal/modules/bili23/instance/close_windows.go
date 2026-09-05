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

// postCloseTo 向指定进程归属的可见顶层窗口投递 WM_CLOSE。
//
// 上游语义（src/gui/interface/main_window.py 实证）：closeEvent → on_close 按用户
// "关闭窗口"设置三分叉——EXIT 放行退出流程（停下载线程、任务队列落盘后才结束）；
// MINIMIZE 仅 hide 收入托盘；ALWAYS_ASK（默认）弹出 ExitDialog 模态询问。
// 因此 WM_CLOSE 是"尽力优雅"而非"必然退出"，退出与否由引擎 Quit 的三态结果如实上报。
//
// 按 PID 精确投递（信使唤窗走上游 QLocalServer 协议，不依赖窗口枚举）。
// 主窗口隐藏（驻托盘）时枚举不到可见窗口 → 静默无操作，不会误伤其他应用窗口。
func postCloseTo(pid uint32) {
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd); length == 0 {
			return 1
		}
		var wpid uint32
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // 继续枚举：多窗口场全部送达
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
}
