//go:build windows

package instance

import (
	win "hanxi/internal/platform/windows"
)

var (
	procPostMessageW = modUser32.NewProc("PostMessageW")
)

const wmClose = 0x0010

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言"按目标 PID
// 收敛"的投递对象——误伤用户窗口是事故，此账目必须可测）。返回成功投递的
// 窗口数（0 = 无可投递窗口，externalquit 优雅段据此如实报错）。
var postCloseFn = postCloseForPID

// postCloseForPID 向归属指定 PID 的可见带标题顶层窗口投递 WM_CLOSE。
// 刻意只投目标 PID（区别于按类名 SunAwtFrame 全量广播——会误伤其他 Java
// 程序窗口）。取消边界如实标注：Termora 开着活动会话时关窗会弹断开确认
// 框（Swing beforeClose），WM_CLOSE 被模态框挡住不再退出——调用方以宽限
// 期收口：自有实例走 JobObject 强杀兜底（远端会话由超时回收），外部实例
// 走 externalquit confirm-force 档在用户明确知情后执行。窗口不存在返回 0。
func postCloseForPID(pid uint32) int {
	if pid == 0 {
		return 0
	}
	count := 0
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return true
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return true
		}
		if wpid == pid {
			if posted, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0); posted != 0 {
				count++
			}
		}
		return true // 继续枚举：目标进程多窗口全部送达
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
