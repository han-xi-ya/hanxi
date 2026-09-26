//go:build windows

package instance

import (
	"strings"
	"syscall"
	"unsafe"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32     = syscall.NewLazyDLL("user32.dll")
	procPostMsg   = modUser32.NewProc("PostMessageW")
	procIsVisible = modUser32.NewProc("IsWindowVisible")
	procTextLen   = modUser32.NewProc("GetWindowTextLengthW")
	procGetText   = modUser32.NewProc("GetWindowTextW")
)

const wmClose = 0x0010

// postCloseByPID 向指定进程的标题为 "DBX" 的可见顶层主窗投递 WM_CLOSE
// （尽力优雅退出）：DBX 关窗语义 = tauri CloseRequested → 按用户设置驻托盘
// 或弹询问确认框——模态框/驻托盘场进程不会在宽限内自然收口，这正是要
// JobObject 强杀兜底的场（宽限期见 instance.go closeGracePeriod），
// 用户侧指引文案由 service 层如实预告（QuitAdvisory）。
//
// 投递对象刻意按主窗标题精确匹配而非"该 PID 全部顶层窗"：单实例插件的
// 隐藏消息窗（-sic/-siw）不属于主窗判据面，close-on-message-window 那条
// ccswitch 实证路径在 DBX 上游未验证，不照搬（决策注记见 instance.go
// NewEngine 的 QuitHook 注释）。窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid != pid {
			return true
		}
		if v, _, _ := procIsVisible.Call(hwnd); v == 0 {
			return true
		}
		if strings.EqualFold(windowText(hwnd), mainWindowTitle) {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true
	})
}

// hasTitleWindowByPIDs 这些进程中是否存在可见且标题为 "DBX" 的顶层窗
// （就绪/主窗在场判据；大小写不敏感比对防上游措辞微调）。消息窗类
// com.dbx.app-sic 的窗口名是 -siw，天然匹配不上标题 "DBX"，无需额外排除
// ——判据面"勿当主窗枚举"由标题精确性天然守住。
func hasTitleWindowByPIDs(pids []uint32) bool {
	if len(pids) == 0 {
		return false // 无进程在场免枚举
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	found := false
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if _, ok := set[wpid]; !ok {
			return true
		}
		if v, _, _ := procIsVisible.Call(hwnd); v == 0 {
			return true
		}
		if strings.EqualFold(windowText(hwnd), mainWindowTitle) {
			found = true
			return false // 命中即停枚举
		}
		return true
	})
	return found
}

// windowText 读取窗口标题（GetWindowTextW 两拍：先量长再取文）。
// 标题为 0 长的窗（消息窗/无名窗）直接空串返回，不发起取文调用。
func windowText(hwnd uintptr) string {
	n, _, _ := procTextLen.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽，池上限 2000 且永不回收——定时
// 炸弹，根治模板见 winfocus_windows.go）。visit 返回 false 停止枚举；
// 属主 PID 查询失败的顶层窗由公共件跳过，与旧 "r == 0" 判据语义一致。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
