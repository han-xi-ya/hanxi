//go:build windows

package instance

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	procFindWnd = modUser32.NewProc("FindWindowW")
	procPostMsg = modUser32.NewProc("PostMessageW")
)

const (
	// classTray/nameTray 上游托盘消息窗口（MainAppWindow）：类名 "TrayWindow"
	// （constants.hpp TRAY_WINDOW）+ 标题 APP_NAME（release 构建 = "TranslucentTB"）。
	// 二者均已在 2026.2 便携 exe 二进制字符串实证。
	classTray = "TrayWindow"
	nameTray  = "TranslucentTB"
	wmClose   = 0x0010
)

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言投递目标——
// 名对不等于"是我托管的那个进程"，WM_CLOSE 误中机主自启的 TranslucentTB 是
// 事故：上游收到即 Exit() 并写配置，常驻小工具被静默退出；测试替身（cmd.exe）
// 没有 TrayWindow，按类名全局投递恰好会打中机主在场的那个真实例。此账目
// 必须可测，模板同 guoheview 接缝。对应 TROUBLESHOOTING #148（待登记）。
var postCloseFn = postCloseForPID

// postCloseForPID 向**归属指定 PID** 的 TranslucentTB 托盘消息窗口投递 WM_CLOSE：
// 上游 MessageHandler 收到即 Exit()（SaveConfig + Shutdown → PostQuitMessage），
// 是保存设置后的标准优雅退出通道。
//
// 判据三层，缺一不可：类名+窗名命中（"TrayWindow" 是极通用的类名，非本应用
// 独占，保留防投错窗）→ 属主 PID == 目标 PID（决定性判据，2026-11-04 加固：
// 旧版只校验属主映像名，而机主自启实例与我方托管实例映像名恒同——"名对"永远
// 区分不出"是哪个"）→ 属主映像名复核 TranslucentTB.exe。
// 刻意不照抄 guoheview 的"可见+标题非空"过滤：TrayWindow 是托盘型常驻件
// 的消息窗口，很可能不可见，可见性判据会把自家窗漏掉。
// 窗口不存在（启动初期/已退出）、pid==0（无受管实例）或任一判据不命中时静默返回。
func postCloseForPID(pid uint32) {
	if pid == 0 {
		return // 无受管实例：绝不按类名全局广播
	}
	// 纯 ASCII 常量无 surrogate 对，UTF16PtrFromString 不可能失败（忽略 err）
	cls, _ := syscall.UTF16PtrFromString(classTray)
	name, _ := syscall.UTF16PtrFromString(nameTray)
	hwnd, _, _ := procFindWnd.Call(
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(name)),
	)
	if hwnd == 0 {
		return
	}
	if !ownerMatchesTarget(windows.HWND(hwnd), pid) {
		return
	}
	_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
}

// ownerMatchesTarget 属主双重校验：属主 PID 必须等于目标 PID，且属主进程映像
// 名必须为 TranslucentTB.exe（PROCESS_QUERY_LIMITED_INFORMATION 最小权限读取；
// 外部实例同为 asInvoker 中等完整性，不受 UIPI 阻断）。
func ownerMatchesTarget(hwnd windows.HWND, target uint32) bool {
	var owner uint32
	windows.GetWindowThreadProcessId(hwnd, &owner)
	if owner == 0 || owner != target {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, owner)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32*syscall.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return false
	}
	base := filepath.Base(syscall.UTF16ToString(buf[:size]))
	return strings.EqualFold(base, exeName)
}
