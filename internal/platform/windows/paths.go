//go:build windows

package windows

import "os"

// SystemRoot 返回 Windows 系统目录（SystemRoot 环境变量，缺失时回退默认安装路径）。
// 收敛各调用点手写的 os.Getenv("SystemRoot") + `C:\Windows` 兜底，保证口径一致。
func SystemRoot() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root
}
