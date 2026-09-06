//go:build !windows

package wechat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// revealInFolder 在系统文件管理器中定位文件（非 Windows 兜底实现）。
// darwin 用 open -R 原生选中语义；Linux 无通用选中协议，退化为打开所在目录。
func revealInFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("文件不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("目标不是普通文件")
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}

func openAttachmentFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("文件不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("目标不是普通文件")
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
