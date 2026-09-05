//go:build !windows

package instance

// hasVisibleWindowByPIDs / focusWindowsByPIDs 非 Windows 平台无窗口枚举语义
// （Hanxi 实际仅在 Windows 运行，保持跨平台可编译）：恒为 false / 0。
func hasVisibleWindowByPIDs(map[uint32]struct{}) bool { return false }
func focusWindowsByPIDs(map[uint32]struct{}) int      { return 0 }
