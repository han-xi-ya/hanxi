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
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return true
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd); length == 0 {
			return true
		}
		if wpid == pid {
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
