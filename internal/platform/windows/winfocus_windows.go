//go:build windows

package windows

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// 外部实例唤窗公共件（N3 Wave 4X 收口核心）。病灶档案（普查底账
// 2026-09-24）：五个模块的 restoreWindowByPID/focusWindowsByPIDs 同款克隆把
// "IsWindowVisible==0" 误当"未最小化"判据——最小化窗 WS_VISIBLE 恒真，
// SW_RESTORE 永不触发，裸 SetForegroundWindow 对 iconic 窗只闪任务栏；
// 主窗藏托盘时又遭前台锁拒绝。本件按 guoheview 标杆收三要素：
//  1. 过滤：可见 + 标题非空（不掀上游刻意隐藏的辅助窗/IME 宿主窗）；
//  2. 恢复：IsIconic 才 SW_RESTORE（不再误伤最大化布局）；
//  3. 置前：SetForegroundForce（AttachThreadInput 借前台特权，托盘后台态可用）。
var (
	procWFEnumWindows      = modUser32Fg.NewProc("EnumWindows")
	procWFGetWinPID        = modUser32Fg.NewProc("GetWindowThreadProcessId")
	procWFIsVisible        = modUser32Fg.NewProc("IsWindowVisible")
	procWFIsIconic         = modUser32Fg.NewProc("IsIconic")
	procWFTitleLen         = modUser32Fg.NewProc("GetWindowTextLengthW")
	procWFShowWindow       = modUser32Fg.NewProc("ShowWindow")
	procSwitchToThisWindow = modUser32Fg.NewProc("SwitchToThisWindow")
)

const swRestoreWin = 9 // SW_RESTORE

// wfEnumCtx 每次枚举的调用方状态。经 lParam 携带的是**自增 ID**而非指针
// （uintptr→unsafe.Pointer 直转是 vet unsafeptr 禁忌；ID→sync.Map 查表同样
// 零闭包零槽消耗，且并发/重入枚举各持各的 ctx、查无此 id 一律停枚举）。
type wfEnumCtx struct {
	visit func(hwnd uintptr, pid uint32) bool
}

var (
	wfCtxSeq atomic.Uint64
	wfCtxMap sync.Map // id(uintptr) → *wfEnumCtx
)

// wfEnumProc 包级唯一回调。**必须静态**：Go/Windows 的
// syscall.NewCallback 池上限 2000 槽且按闭包 funcval 去重、永不回收
// （审查 P0 实证：每次调用现场造闭包注册 = 每次烧一槽，常驻托盘应用
// 千余次唤窗/探测后 `fatal error: too many callback functions` 硬崩、
// 不可 recover）。本文件是全仓该模式的根治模板。
var wfEnumProc = syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
	v, ok := wfCtxMap.Load(lParam)
	if !ok {
		return 0 // 未知/已回收的枚举上下文：立即停止，宁可不服务不乱调
	}
	ctx := v.(*wfEnumCtx)
	var pid uint32
	if r, _, _ := procWFGetWinPID.Call(hwnd, uintptr(unsafe.Pointer(&pid))); r == 0 {
		return 1 // 拿不到属主 PID 的窗（极罕见异常态）跳过继续枚举
	}
	if !ctx.visit(hwnd, pid) {
		return 0 // 回调要求停止枚举
	}
	return 1
})

// EnumTopWindows 枚举全部顶层窗口（回调返 false 提前停止）。
// 同步执行：调用期间 ctx 由本帧持有，无逃逸风险。
func EnumTopWindows(visit func(hwnd uintptr, pid uint32) bool) {
	id := wfCtxSeq.Add(1)
	wfCtxMap.Store(id, &wfEnumCtx{visit: visit})
	defer wfCtxMap.Delete(id)
	procWFEnumWindows.Call(wfEnumProc, uintptr(id))
}

// focusableTopWindow 判定"可作为唤窗目标的顶层窗"：可见且标题非空。
// 最小化窗仍满足（WS_VISIBLE 恒真）——正是必须配 IsIconic 恢复的原因。
func focusableTopWindow(hwnd uintptr) bool {
	if v, _, _ := procWFIsVisible.Call(hwnd); v == 0 {
		return false
	}
	if t, _, _ := procWFTitleLen.Call(hwnd); t == 0 {
		return false
	}
	return true
}

// FocusTopWindowForPIDs 在给定 PID 集内唤一个可聚焦顶层窗：先摆到枚举序首位
// 候选（可见+有标题），IsIconic 则 SW_RESTORE，再 SetForegroundForce 置前。
// 命中并成功置前返回 true；无候选窗返回 false（调用方决定拉新实例等后手）。
// 只动第一个命中窗即停枚举——多窗应用唤"哪个"由上游 z-order 决定，不自作主张。
func FocusTopWindowForPIDs(pids map[uint32]struct{}) bool {
	done := false
	EnumTopWindows(func(hwnd uintptr, pid uint32) bool {
		if _, ok := pids[pid]; !ok {
			return true
		}
		if !focusableTopWindow(hwnd) {
			return true
		}
		if ic, _, _ := procWFIsIconic.Call(hwnd); ic != 0 {
			procWFShowWindow.Call(hwnd, swRestoreWin)
		}
		if err := SetForegroundForce(hwnd); err == nil {
			done = true
			return false
		}
		// 借权失败仍视同唤回（前台锁极端场景）：SwitchToThisWindow 无权限要求兜底。
		procSwitchToThisWindow.Call(hwnd, 1)
		done = true
		return false
	})
	return done
}

// FocusTopWindowForPID 单进程便捷口。
func FocusTopWindowForPID(pid uint32) bool {
	return FocusTopWindowForPIDs(map[uint32]struct{}{pid: {}})
}

// FocusAllTopWindowsForPIDs 唤起 PID 集的**每个**可聚焦顶层窗（多会话窗应用
// 如 rustdesk：远控会话窗逐个恢复置前），判据与恢复动作同 FocusTopWindowForPIDs
// （可见+标题过滤、IsIconic 才恢复、借前台特权置前）。返回命中窗数。
// 单主窗应用勿用本口——逐个置前只会让最后一个赢，用首停版本语义更干净。
func FocusAllTopWindowsForPIDs(pids map[uint32]struct{}) int {
	count := 0
	EnumTopWindows(func(hwnd uintptr, pid uint32) bool {
		if _, ok := pids[pid]; !ok {
			return true
		}
		if !focusableTopWindow(hwnd) {
			return true
		}
		if ic, _, _ := procWFIsIconic.Call(hwnd); ic != 0 {
			procWFShowWindow.Call(hwnd, swRestoreWin)
		}
		if err := SetForegroundForce(hwnd); err != nil {
			procSwitchToThisWindow.Call(hwnd, 1)
		}
		count++
		return true
	})
	return count
}

// HasFocusableTopWindowForPIDs 报告 PID 集内是否存在可唤顶层窗（可见+有标题，
// 含最小化）。替代各模块"以 Visible 判有窗"的同款误判：那正是
// "外部实例在场但唤不动"的账面源头——本件判在唤窗同一口径上，两问共用一判。
func HasFocusableTopWindowForPIDs(pids map[uint32]struct{}) bool {
	found := false
	EnumTopWindows(func(hwnd uintptr, pid uint32) bool {
		if _, ok := pids[pid]; !ok {
			return true
		}
		if focusableTopWindow(hwnd) {
			found = true
			return false
		}
		return true
	})
	return found
}
