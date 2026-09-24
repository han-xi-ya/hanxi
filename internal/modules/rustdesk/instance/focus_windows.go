//go:build windows

package instance

import (
	win "hanxi/internal/platform/windows"
)

// 远控会话窗唤回（N3 Wave 4X 收口）：委托平台公共件，保留"每窗逐个唤"的
// rustdesk 多会话语义（远控窗口每会话一窗，唤全部而非首窗，等价托盘双击）。
// 旧实现两处病灶由公共件根除：①"IsWindowVisible==0 才 SW_RESTORE"把可见性
// 误当未最小化判据（iconic 窗 WS_VISIBLE 恒真，恢复永不触发）；②裸
// SetForegroundWindow 在 hanxi 藏托盘时遭前台锁拒绝。同时新判据剔除
// 无标题隐藏窗——不再把上游刻意隐藏的宿主窗掀出来。

// hasVisibleWindowByPIDs 这些进程中是否存在可唤顶层窗（含最小化；与唤窗同判据）。
func hasVisibleWindowByPIDs(set map[uint32]struct{}) bool {
	return win.HasFocusableTopWindowForPIDs(set)
}

// focusWindowsByPIDs 唤起这些进程的全部可聚焦顶层窗，返回命中窗数。
func focusWindowsByPIDs(set map[uint32]struct{}) int {
	return win.FocusAllTopWindowsForPIDs(set)
}
