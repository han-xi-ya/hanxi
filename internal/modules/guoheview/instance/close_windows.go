//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

const wmClose = 0x0010

// postCloseForPID 向归属指定 PID 的可见带标题顶层窗口投递 WM_CLOSE。
// 真机实测（3.2.7）：果核看图关窗即退（无托盘驻留），全部窗口收到 WM_CLOSE
// 后 3 秒内进程退出——优雅退出通道存在且有效（与 piclite"关窗藏托盘"相反）。
// 刻意只投自有托管实例的 PID：上游是多实例应用，用户双击图片自行打开的
// 看图窗口绝不越权关闭（区别于 recordly 的单实例全名投递——语义在此会误伤）。
// 窗口不存在（启动初期/已退出）静默返回。
func postCloseForPID(pid uint32) {
	if pid == 0 {
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
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // 继续枚举：自有进程多窗口场全部送达
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
}
