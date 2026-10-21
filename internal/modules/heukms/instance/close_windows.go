//go:build windows

package instance

import (
	"errors"
	"syscall"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	procPostMsg = modUser32.NewProc("PostMessageW")
)

const (
	wmClose = 0x0010

	// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
	// 直接失败码——未提权的 Hanxi 拉起 HEU 时得到它（不会代弹 UAC）。
	// 清单事实由 PE 资源 MANIFEST/1/2052 实证：level="requireAdministrator"、
	// uiAccess="false"（取证过程见 docs/TROUBLESHOOTING.md 的 HEU 条目）。
	errorElevationRequired syscall.Errno = 740
)

// postCloseByPID 向指定进程的所有顶层窗口投递 WM_CLOSE（尽力优雅退出）。
//
// HEU 关窗是否即退进程无法静态取证（上游闭源、代码段 UPX 压缩，读不到窗口
// 消息处理逻辑），因此这里只把它当作**尽力而为的优雅通道**：宽限期内进程收口
// 最好，收不了由内核 JobObject 强杀兜底——两条腿都在，不赌上游行为。
// 窗口不存在（启动初期/已退出）静默返回。
func postCloseByPID(pid uint32) {
	forEachWindow(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			_, _, _ = procPostMsg.Call(hwnd, wmClose, 0, 0)
		}
		return true // 继续枚举
	})
}

// restoreWindowByPID 唤起指定进程的主窗口（委托平台公共件：按可见+标题过滤、
// IsIconic 恢复、借权置前台三要素收口，与 litemonitor 同一根治模板）。
func restoreWindowByPID(pid uint32) {
	win.FocusTopWindowForPID(pid)
}

// hasVisibleWindowByPIDs 这些进程中是否存在可唤顶层窗口（与唤窗同一判据，
// 含最小化在场；隐藏辅助窗不计数——service 的"窗口未见"提示与唤窗分流共用）。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	return win.HasFocusableTopWindowForPIDs(set)
}

// elevateHint 识别提权需求错误：HEU 的 manifest 为 requireAdministrator，
// 未提权父进程 CreateProcess 直接失败（740）且系统不代弹 UAC。
// 返回面向用户的指引文案；非该错误返回空串。
//
// 跨层契约：文案必须含「管理员」关键词——前端 ElevateRestart 组件据此判定
// 一键提权重启按钮的显隐（与 bcu/litemonitor 基准一致，改写不得丢关键词）。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "HEU KMS Activator 要求管理员权限运行（上游 manifest 强制）：请以管理员身份重新启动 Hanxi 后重试"
	}
	return ""
}

// ElevateHint 是 elevateHint 的导出别名，供 service 层在 **RPC 错误通道**复用同一
// 份判据与文案：事件快照那条由引擎 wrapper 改写，RPC 这条若不共用，前端在某些
// 展示路径（直接渲染 RPC 错误而非快照 Error）上就会丢掉一键提权重启按钮。
func ElevateHint(err error) string { return elevateHint(err) }

// forEachWindow 统一封装顶层窗枚举：委托平台公共件静态回调（勿现场
// syscall.NewCallback 注册闭包——回调槽池上限 2000 且永不回收，根治模板见
// winfocus_windows.go）。visit 返回 false 停止枚举。
func forEachWindow(visit func(hwnd uintptr, wpid uint32) bool) {
	win.EnumTopWindows(visit)
}
