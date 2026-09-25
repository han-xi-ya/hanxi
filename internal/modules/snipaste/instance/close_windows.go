//go:build windows

package instance

import (
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32        = syscall.NewLazyDLL("user32.dll")
	procPostMessageW = modUser32.NewProc("PostMessageW")
)

const wmClose = 0x0010

// postCloseByPID 向自有 PID 的全部顶层窗口投递 WM_CLOSE。
// Snipaste 是闭源 Qt 托盘程序，此操作仅是尽力关闭请求，不等价于已证实的退出协议。
func postCloseByPID(pid uint32) int {
	count := 0
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			if posted, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0); posted != 0 {
				count++
			}
		}
		return true // 继续枚举
	})
	return count
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r != 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
