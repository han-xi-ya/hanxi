//go:build windows

package instance

import (
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32        = syscall.NewLazyDLL("user32.dll")
	procPostMsg      = modUser32.NewProc("PostMessageW")
	procIsWinVisible = modUser32.NewProc("IsWindowVisible")
)

const wmClose = 0x0010

// postCloseByPID 向指定进程的所有可见顶层窗口投递 WM_CLOSE：
// VS Code（Electron）关最后一个窗口即退出主进程（未保存内容有 hot exit
// 备份兜底）；宽限期后仍有窗口未响应（模态对话框等）由 JobObject 强杀兜底。
// 窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			if visible, _, _ := procIsWinVisible.Call(hwnd); visible != 0 {
				_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
			}
		}
		return true // 继续枚举（同 PID 可能多窗口：编辑器主窗 + 浮动面板窗）
	})
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r == 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
