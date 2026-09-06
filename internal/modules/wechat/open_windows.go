//go:build windows

package wechat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// revealInFolder 在资源管理器中定位文件（打开所在目录并选中）。
// explorer.exe 收文件参数语义是"打开/执行"而非"定位"，必须走 /select,；
// 且 /select, 与路径必须是同一个参数（逗号是语法一部分，见 TROUBLESHOOTING #11）。
func revealInFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("文件不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("目标不是普通文件")
	}
	return exec.Command("explorer.exe", "/select,"+filepath.Clean(path)).Start()
}

func openAttachmentFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("文件不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("目标不是普通文件")
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Start()
}
