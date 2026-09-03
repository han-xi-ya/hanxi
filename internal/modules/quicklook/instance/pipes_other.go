//go:build !windows

package instance

import "fmt"

// sendPipeMessage 非 Windows 平台占位：QuickLook 本就是 Windows 专属工具，
// 命名管道控制通道不存在，恒报告不可用，令 Quit 直接走强杀兜底路径。
func sendPipeMessage(_ string) error {
	return fmt.Errorf("命名管道控制仅在 Windows 可用")
}
