//go:build windows

package instance

import (
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procIsWinVisible     = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen = modUser32.NewProc("GetWindowTextLengthW")
	procPostMsg          = modUser32.NewProc("PostMessageW")
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
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return true
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return true
		}
		if pids[wpid] {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举：多窗口场全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r != 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
