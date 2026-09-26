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

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）：
// GoNavi 关窗语义 = OnBeforeClose——未保存 SQL 草稿在场时会弹确认框，模态框
// 挂住消息循环时主窗的 WM_CLOSE 只能排队，进程不会在宽限内自然收口——
// 这正是要 JobObject 强杀兜底的场（宽限期见 instance.go closeGracePeriod），
// 用户侧指引文案由 service 层如实预告。窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口（N3 收口：委托平台公共件）。
// GoNavi 无单实例信使协议（二次拉起=并存多开），此路径是"打开窗口"的唯一实现。
// 公共件判据为"可见+非空标题首窗"——GoNavi 常态只有主窗在场，标题精确匹配
// 的对话框误唤半径可忽略；判据与恢复/借权置前三要素收口在 winfocus，不在本层
// 重造（根治模板见 winfocus_windows.go）。
func restoreWindowByPID(pid uint32) {
	win.FocusTopWindowForPID(pid)
}

// hasTitleWindowByPIDs 这些进程中是否存在可见且标题为 "GoNavi" 的顶层窗
// （就绪/主窗在场判据；大小写不敏感比对防上游措辞微调）。
func hasTitleWindowByPIDs(pids []uint32) bool {
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
