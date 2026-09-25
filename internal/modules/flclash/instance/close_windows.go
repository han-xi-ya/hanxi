//go:build windows

package instance

import (
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	procPostMsg = modUser32.NewProc("PostMessageW")
)

const (
	wmClose = 0x0010
)

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// Flutter 默认关窗即进程退出；若上游改为驻托盘则宽限后强杀兜底。
// 窗口不存在（启动初期/已退出）静默返回。Flutter 窗口类名不可预测，按 PID 枚举。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口（N3 收口：委托平台公共件——
// 可见+标题过滤、IsIconic 恢复、借前台特权置前；旧 "Visible==0 才恢复"
// 判据永不触发 SW_RESTORE 的病灶在公共件根除）。
// FlClash 上游二次启动无唤窗行为，此路径是"打开窗口"的唯一实现。
func restoreWindowByPID(pid uint32) {
	win.FocusTopWindowForPID(pid)
}

// hasVisibleWindowByPIDs 是否存在可唤顶层窗（空闲退出豁免信号；与唤窗同一
// 判据含最小化在场，隐藏辅助窗不计数）。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	return win.HasFocusableTopWindowForPIDs(set)
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r == 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
