//go:build !windows

package instance

// postCloseByPID / hasVisibleWindowByPID 非 Windows 平台无窗口枚举
// （HubKit 实际仅在 Windows 运行，保持跨平台可编译）：恒为 no-op / false。
func postCloseByPID(uint32)             {}
func hasVisibleWindowByPID(uint32) bool { return false }
