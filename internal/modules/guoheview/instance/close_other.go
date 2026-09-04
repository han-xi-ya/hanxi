//go:build !windows

package instance

// postCloseForPID 非 Windows 平台占位：无窗口通道，Quit 直接走强杀路径。
func postCloseForPID(pid uint32) {}
