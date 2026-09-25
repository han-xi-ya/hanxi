//go:build windows

package instance

import (
	win "hanxi/internal/platform/windows"
)

const wmClose = 0x0010

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言"按自有 PID
// 收敛"的投递目标——误伤用户窗口是事故，此账目必须可测）。
var postCloseFn = postCloseForPID

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
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return true
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return true
		}
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举：自有进程多窗口场全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r != 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
