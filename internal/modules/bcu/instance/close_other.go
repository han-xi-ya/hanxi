//go:build !windows

package instance

// postCloseByPID / hasVisibleWindowByPID / elevateHint 非 Windows 平台无窗口
// 枚举与提权语义（Hanxi 实际仅在 Windows 运行，保持跨平台可编译）：
// 恒为 no-op / false / 空串。
func postCloseByPID(uint32)             {}
func hasVisibleWindowByPID(uint32) bool { return false }
func elevateHint(error) string          { return "" }
