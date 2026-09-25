//go:build windows

package instance

import (
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	closeUser32     = syscall.NewLazyDLL("user32.dll")
	procPostMessage = closeUser32.NewProc("PostMessageW")
)

const wmClose = 0x0010

// postCloseByPID 向指定 MangoDisk 进程的全部顶层窗口投递 WM_CLOSE。
// 单实例 signal window 只用于确定 PID，不把它误当作主窗口。
func postCloseByPID(pid uint32) {
	if pid == 0 {
		return
	}
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMessage.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举：目标进程多窗口全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "ret != 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
