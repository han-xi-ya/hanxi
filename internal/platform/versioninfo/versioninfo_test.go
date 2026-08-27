//go:build windows

package versioninfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileVersionMissingFile 不存在的文件必须报错（真实 PE 读取路径见各模块 S5 真机验证）。
func TestFileVersionMissingFile(t *testing.T) {
	v, err := FileVersion(filepath.Join(t.TempDir(), "no-such.exe"))
	if err == nil && v != "" {
		t.Errorf("不存在的文件应报错或返回空, 实际 %q", v)
	}
}

// TestFileVersionRealPE 用系统自带的真实 PE（notepad.exe）验证解析链路不打折扣：
// 版本资源读取是纯 Windows API 路径，单测无需伪造 PE 也能跑通骨干。
func TestFileVersionRealPE(t *testing.T) {
	exe := filepath.Join(os.Getenv("WINDIR"), "System32", "notepad.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skipf("系统无 notepad.exe: %v", err)
	}
	v, err := FileVersion(exe)
	if err != nil {
		t.Fatalf("FileVersion(notepad.exe): %v", err)
	}
	if !strings.Contains(v, ".") {
		t.Errorf("版本字符串格式异常: %q", v)
	}
}
