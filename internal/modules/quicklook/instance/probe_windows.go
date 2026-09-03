//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

// mutexName QuickLook 单实例命名互斥体（App.xaml.cs EnsureFirstInstance 内
// `new Mutex(true, "QuickLook.App.Mutex", ...)`）。非 "Global\" 前缀 = 会话级；
// 托管与用户同为交互式登录会话，命名空间一致。主实例存活期间互斥体存在。
const mutexName = "QuickLook.App.Mutex"

type windowsQuickLookProbe struct{}

// NewQuickLookProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewQuickLookProbe() QuickLookProbe { return &windowsQuickLookProbe{} }

func (p *windowsQuickLookProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsQuickLookProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}
