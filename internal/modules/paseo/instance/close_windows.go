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

const (
	wmClose = 0x0010
)

// forEachPaseoWindow 枚举归属 Paseo.exe 进程、可见且带标题的顶层窗口。
// Electron 窗口类名是 Chromium 通用的 Chrome_WidgetWin_1（不能当身份，
// 按类名 FindWindow 会误伤用户浏览器），必须 EnumWindows + 进程名过滤
// （recordly 同纪律）。要求标题非空以排除启动早期隐藏宿主与无题辅助窗口。
// visitor 返回 false 停止枚举。
func forEachPaseoWindow(visit func(hwnd uintptr) bool) {
	pids := paseoPIDs()
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
		if !pids[wpid] {
			return true
		}
		return visit(hwnd)
	})
}

// postClose 向所有归属 Paseo.exe 的可见带标题顶层窗口投递 WM_CLOSE。
// 上游语义（main.ts 实证）：Windows 无托盘，主窗口关闭 → window-all-closed
// → app.quit()，before-quit 走 daemon 清理，优雅退出可达；多工作区窗口场
// 全部送达，宽限 + JobObject 强杀兜底收敛。无窗口时静默返回。
//
// 共享数据同锁组下"自有 running"与"外部实例"互斥，本函数只会命中唯一主
// 实例的窗口，不存在误伤外部实例的窗口面（external 态 service 层根本不调 Quit）。
func postClose() {
	forEachPaseoWindow(func(hwnd uintptr) bool {
		_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		return true // 继续枚举：多窗口场全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r == 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
