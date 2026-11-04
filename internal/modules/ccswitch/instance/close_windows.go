//go:build windows

package instance

import (
	"syscall"
	"unsafe"
)

var (
	modUser32                    = syscall.NewLazyDLL("user32.dll")
	procFindWnd                  = modUser32.NewProc("FindWindowW")
	procPostMsg                  = modUser32.NewProc("PostMessageW")
	procIsWinVisible             = modUser32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
)

const (
	classCCSwitch = "com.ccswitch.desktop-sic" // 单实例消息窗口类名（identifier + "-sic"）
	nameCCSwitch  = "com.ccswitch.desktop-siw" // 单实例消息窗口名（identifier + "-siw"）
	wmClose       = 0x0010
)

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言"按自有 PID
// 收敛"的投递目标——误伤机主自己启动的 CC Switch 是事故，此账目必须可测，
// 见 docs/TROUBLESHOOTING.md #148）。
var postCloseFn = postCloseForPID

// postCloseForPID 向**属主就是我方受管实例**的单实例消息窗口投递 WM_CLOSE：
// tauri on_window_event 全局拦截 CloseRequested，按用户的托盘设置执行
// exit(0)（关窗即退）或 hide（驻留托盘）——这正是"尽力优雅退出"的语义。
// 窗口不存在（启动初期/已退出）静默返回。
//
// 类名/窗名是全局唯一（identifier 派生），FindWindowW 本身**不带归属过滤**：
// 机主手动启动的 CC Switch 同样持有这个名字的窗口，旧版"找到就投"等于替用户
// 关他自启的实例。故取到 hwnd 后必须复核属主 PID == 受管 PID 才投递——钩子只在
// 内核确认自有进程在世时被调用，此刻名字与 PID 两条判据同时成立的窗口才是我们
// 拉起的那一个（guoheview/bcu/paseo 同纪律）。
func postCloseForPID(pid uint32) {
	if pid == 0 {
		return
	}
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
	var wpid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
	if wpid != pid {
		return // 名字对但主人不是我们——机主自启的实例，绝不代关
	}
	_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
}
