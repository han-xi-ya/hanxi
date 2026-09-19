//go:build windows

package version

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestRemoveRefusedWhileExeLocked 文件锁拒卸：版本目录内 rufus.exe 被非共享
// 句柄占用（模拟"正在运行"对文件系统的锁效应）时，Tree.Remove 的 rename 隔离
// 必须失败并给出人话错误，目录保持原样；解锁后方可卸载。
// （服务层在用拒卸走 engine.Snapshot 版本比对先行拦截；本用例把文件系统
// 层的最后一道拒卸防线钉死——提权运行的 Rufus 实例句柄同样锁 exe。）
func TestRemoveRefusedWhileExeLocked(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v4.9", "locked-exe")

	exePath := filepath.Join(m.versionsDir, "rufus_4.9", exeName)
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

	if rmErr := m.Remove("v4.9"); rmErr == nil {
		_ = windows.CloseHandle(h)
		t.Fatal("占用中的版本目录应拒绝卸载")
	} else if !strings.Contains(rmErr.Error(), "无法卸载") {
		t.Errorf("拒卸错误应走内核人话口径: %v", rmErr)
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v4.9"); err != nil {
		t.Fatalf("解锁后应可卸载: %v", err)
	}
}
