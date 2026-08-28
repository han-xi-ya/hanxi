//go:build windows

package wechat

import (
	"fmt"
	"os"
	"os/exec"
)

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
