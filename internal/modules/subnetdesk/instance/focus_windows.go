//go:build windows

package instance

import (
	win "hanxi/internal/platform/windows"
)

// 唤窗公共件委托（N3 Wave 4X 收口，与 rustdesk 同族同修）：旧实现把
// "IsWindowVisible==0" 当最小化判据（iconic 窗恒 WS_VISIBLE，SW_RESTORE 永不
// 触发）+ 裸 SetForegroundWindow 遭前台锁——"点了没反应"两连病灶根除；
// 新判据同时剔除无标题隐藏窗。多窗逐个唤与 rustdesk 同语义保留。

// hasVisibleWindowByPIDs 这些进程中是否存在可唤顶层窗（含最小化；与唤窗同判据）。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	return win.HasFocusableTopWindowForPIDs(set)
}

// focusWindowsByPIDs 唤起这些进程的全部可聚焦顶层窗，返回命中窗数。
func focusWindowsByPIDs(set map[uint32]struct{}) int {
	return win.FocusAllTopWindowsForPIDs(set)
}
