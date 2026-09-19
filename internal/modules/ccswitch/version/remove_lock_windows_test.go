//go:build windows

package version

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestRemoveRefusedWhileExeLocked 文件锁拒卸：版本目录内 cc-switch.exe 被非共享
// 句柄占用（模拟"正在运行"对文件系统的锁效应）时，Tree.Remove 的 rename 隔离
// 必须失败并给出人话错误，目录保持原样；解锁后方可卸载。
func TestRemoveRefusedWhileExeLocked(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.19.0", "locked-exe")

	exePath := filepath.Join(m.versionsDir, "ccswitch_3.19.0", exeName)
	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr(exePath),
		windows.GENERIC_READ,
		0, // 不给任何共享模式：句柄在世期间目录不可改名/删除
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if h == windows.InvalidHandle {
		t.Fatalf("构造文件锁失败: %v", err)
	}

	if rmErr := m.Remove("v3.19.0"); rmErr == nil {
		_ = windows.CloseHandle(h)
		t.Fatal("占用中的版本目录应拒绝卸载")
	} else if !strings.Contains(rmErr.Error(), "无法卸载") {
		t.Errorf("拒卸错误应走内核人话口径: %v", rmErr)
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v3.19.0"); err != nil {
		t.Fatalf("解锁后应可卸载: %v", err)
	}
}
