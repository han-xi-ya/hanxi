//go:build !windows

package instance

// postCloseByPID / restoreWindowByPID / hasVisibleWindowByPIDs
// 非 Windows 平台无窗口枚举（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒为 no-op / false。
func postCloseByPID(uint32)                           {}
func restoreWindowByPID(uint32)                       {}
func hasVisibleWindowByPIDs(map[uint32]struct{}) bool { return false }
