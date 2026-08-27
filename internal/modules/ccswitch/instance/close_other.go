//go:build !windows

package instance

// postClose 非 Windows 平台无窗口消息（HubKit 实际仅在 Windows 运行）。
func postClose() {}
