//go:build windows

package instance

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"
)

// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
// 直接失败码——未提权 Hanxi 拉起 RAMMap 时得到它（系统不代弹 UAC）。
const errorElevationRequired syscall.Errno = 740

var (
	procPostMessageW          = modUser32.NewProc("PostMessageW")
	procGetWindowThreadProcID = modUser32.NewProc("GetWindowThreadProcessId")
)

const wmClose = 0x0010

// postCloseFn WM_CLOSE 投递接缝（默认真实实现，单测注入以断言"按目标 PID
// 收敛"的投递对象——误伤用户窗口是事故，此账目必须可测）。返回成功投递的
// 窗口数（0 = 无可投递窗口，externalquit 优雅段据此如实报错）。
var postCloseFn = postCloseForPID

// postCloseForPID 向归属指定 PID 的可见带标题顶层窗口投递 WM_CLOSE。
// 刻意只投目标 PID（区别于按进程名全量广播——多实例上游会误伤用户自开
// 会话窗口）。取消边界如实标注：RAMMap 是零状态观察工具（无模态确认框），WM_CLOSE 直达即退；
// 自有实例走 supervisor 强杀兜底，外部实例按 N3 终裁 force-free 档经
// externalquit 执行（提权目标 UIPI 拦截如实降级）。窗口不存在静默返回 0。
func postCloseForPID(pid uint32) int {
	if pid == 0 {
		return 0
	}
	count := 0
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return 1
		}
		var wpid uint32
		if r, _, _ := procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && wpid == pid {
			if posted, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0); posted != 0 {
				count++
			}
		}
		return 1 // 继续枚举：目标进程多窗口全部送达
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return count
}

// elevateHint 识别提权需求错误：RAMMap manifest 为 requireAdministrator，
// 未提权父进程 CreateProcess 直接失败（740）且系统不代弹 UAC。返回面向用户
// 的指引文案（必须含"管理员"关键词——前端 ElevateRestart 一键提权重启组件
// 据此匹配显隐，跨层契约由家族 TestElevateHint 钉死）；非该错误返回空串。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "RAMMap 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
	}
	return ""
}

// isElevateGuide 断言文案是否提权指引（service 层 stopped 态引导行与测试共用的
// 单源判据）。
func isElevateGuide(msg string) bool { return strings.Contains(msg, "管理员") }
