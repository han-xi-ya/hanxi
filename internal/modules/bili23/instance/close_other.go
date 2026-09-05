//go:build !windows

package instance

// postCloseTo 非 Windows 平台无 WM_CLOSE 信使：静默 no-op（Quit 将如实返回
// 三态中的存活分支，由用户显式 Stop 收敛）。
func postCloseTo(_ uint32) {}
