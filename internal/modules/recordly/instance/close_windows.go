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

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言投递目标收敛
// 为"自有 PID"——误伤用户窗口是事故，此账目必须可测，guoheview 同款形状）。
var postCloseFn = postCloseForPID

// postCloseForPID 向归属指定 PID（我方受管实例）的可见且带标题顶层窗口投递
// WM_CLOSE；pid==0（无自有进程在世）静默返回。可见 + 标题非空两条过滤是上游
// 优雅退出语义所需（编辑器窗收起回 HUD 不成主窗，无标题工具窗不作为退出信号）。
// Electron 窗口类名是 Chromium 通用的 Chrome_WidgetWin_1（与一切 Chromium
// 应用共享），绝不能按类名 FindWindow——会误伤用户浏览器窗口，必须
// EnumWindows + 按属主 PID 过滤（bcu/bili23/airsnip 同族口径：投递只认自有
// 受管 PID）。旧注释"按进程名广播以便覆盖外部实例"与引擎契约冲突——外部实例
// 按 ErrExternal 只感知不管辖（内核 Stop 的 external 分支不走优雅通道），
// 不代关其窗；且按名广播会漏进单测替身进程场景：cmd.exe 替身没有 Recordly
// 窗口，投递却命中机主本机真实运行的 Recordly 并当场关掉它（事故，待登记
// TROUBLESHOOTING #148）。
//
// 上游语义（main.ts 实证）：Windows 无托盘（shouldUseTray 仅 Linux），
// 主窗口关闭 → window-all-closed → app.quit()，优雅退出可达；
// 编辑器窗口关闭 = 收起回 HUD（不退出应用），多窗口场由 Quit 的
// 宽限 + JobObject 强杀兜底收敛。自有进程无窗口（启动初期/已退出）静默返回。
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
		return true // 继续枚举：自有实例多窗口场全部送达
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r != 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
