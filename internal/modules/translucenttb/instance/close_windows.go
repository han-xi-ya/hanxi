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
	// exeName 上游主程序文件名（属主校验用；与 version 子包 exeName 同值，
	// instance 不 import version——保持两子包零依赖分层）。
	exeName = "TranslucentTB.exe"

	// classTray/nameTray 上游托盘消息窗口（MainAppWindow）：类名 "TrayWindow"
	// （constants.hpp TRAY_WINDOW）+ 标题 APP_NAME（release 构建 = "TranslucentTB"）。
	// 二者均已在 2026.2 便携 exe 二进制字符串实证。
	classTray = "TrayWindow"
	nameTray  = "TranslucentTB"
	wmClose   = 0x0010
)

// postClose 向 TranslucentTB 主实例的托盘消息窗口投递 WM_CLOSE：
// 上游 MessageHandler 收到即 Exit()（SaveConfig + Shutdown → PostQuitMessage），
// 是保存设置后的标准优雅退出通道。
//
// "TrayWindow" 是极通用的类名（非本应用独占），故命中后必须校验属主进程
// 映像名确为 TranslucentTB.exe 才投递——防误伤同名窗口类的应用。
// 窗口不存在（启动初期/已退出）或属主校验失败时静默返回。
func postClose() {
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
	if !ownerIsTranslucentTB(windows.HWND(hwnd)) {
		return
	}
	_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
}

// ownerIsTranslucentTB 属主进程映像名校验（PROCESS_QUERY_LIMITED_INFORMATION
// 最小权限读取；外部实例同为 asInvoker 中等完整性，不受 UIPI 阻断）。
func ownerIsTranslucentTB(hwnd windows.HWND) bool {
	var pid uint32
	windows.GetWindowThreadProcessId(hwnd, &pid)
	if pid == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
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
