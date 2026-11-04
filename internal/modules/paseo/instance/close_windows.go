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

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言"按自有 PID
// 收敛"的投递目标）。
//
// 本模块的误伤代价比别家高一个量级：机主日常就挂着 Paseo 干活，投递判据一旦
// 放宽到"按进程名全系统广播"，跑一次单测就把他会话的窗口全关掉（2026-11-04
// 机主实报"跑测试会关闭 paseo 窗口"，根因即此，见 docs/TROUBLESHOOTING.md #148）。
var postCloseFn = postCloseForPID

// postCloseForPID 向**归属我方受管实例 PID**、可见且带标题的顶层窗口投递 WM_CLOSE。
// 上游语义（main.ts 实证）：Windows 无托盘，主窗口关闭 → window-all-closed
// → app.quit()，before-quit 走 daemon 清理，优雅退出可达；多工作区窗口同属主
// 进程，全部送达，宽限 + JobObject 强杀兜底收敛。无窗口时静默返回。
//
// 身份判据取"属主 PID == 受管 PID"而非旧版的"进程名是 Paseo.exe"：Electron 是
// 多进程同名模型，按名广播必然把机主自己启动的实例算进来；而钩子只在内核确认
// "自有进程在世"（Snapshot().Managed）时才被调用，此刻该 PID 就是窗口的唯一合法
// 主人，按 PID 收敛既不漏（自有实例的窗全在这）也不误伤（别人的窗一概不碰）。
// 探针侧的按名枚举（forEachPaseoWindow）保留——只读感知别人的实例是允许的能力，
// 关别人的窗不是。
func postCloseForPID(pid uint32) {
	if pid == 0 {
		return
	}
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid != pid {
			return true
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return true
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return true
		}
		_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		return true // 继续枚举：自有实例的多窗口场全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r == 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
