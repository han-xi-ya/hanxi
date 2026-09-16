//go:build windows

package windows

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RevealDir 在资源管理器中打开指定目录（各托管模块 OpenDir 的公共实现）。
func RevealDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("目标不是目录: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// RevealFile 在资源管理器中打开父目录并高亮定位指定文件（explorer /select 习语）。
func RevealFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("文件路径不能为空")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件不存在或不可访问: %s", path)
	}
	return exec.Command("explorer.exe", "/select,"+path).Start()
}
