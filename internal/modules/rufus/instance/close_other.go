//go:build !windows

package instance

// postCloseByPID / restoreWindowByPID / hasVisibleWindowByPIDs / elevateHint
// 非 Windows 平台无窗口枚举与提权语义（Hanxi 实际仅在 Windows 运行，
// 此处仅保证跨平台编译与单测可跑）。
func postCloseByPID(uint32)                           {}
func restoreWindowByPID(uint32)                       {}
func hasVisibleWindowByPIDs(map[uint32]struct{}) bool { return false }
func elevateHint(error) string                        { return "" }
