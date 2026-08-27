//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32        = syscall.NewLazyDLL("user32.dll")
	procFindWnd      = modUser32.NewProc("FindWindowW")
	procPostMsg      = modUser32.NewProc("PostMessageW")
	procIsWinVisible = modUser32.NewProc("IsWindowVisible")
)

const (
	classCCSwitch = "com.ccswitch.desktop-sic" // 单实例消息窗口类名（identifier + "-sic"）
	nameCCSwitch  = "com.ccswitch.desktop-siw" // 单实例消息窗口名（identifier + "-siw"）
	wmClose       = 0x0010
)

// postClose 向 CC Switch 主实例投递 WM_CLOSE：
// tauri on_window_event 全局拦截 CloseRequested，按用户的托盘设置执行
// exit(0)（关窗即退）或 hide（驻留托盘）——这正是"尽力优雅退出"的语义。
// 窗口不存在（启动初期/已退出）静默返回。
func postClose() {
	// 纯 ASCII 常量无 surrogate 对，UTF16PtrFromString 不可能失败（忽略 err）
	cls, _ := syscall.UTF16PtrFromString(classCCSwitch)
	name, _ := syscall.UTF16PtrFromString(nameCCSwitch)
	hwnd, _, _ := procFindWnd.Call(
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(name)),
	)
	if hwnd == 0 {
		return
	}
	_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
}
