//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

// mutexName BCU 主实例的命名互斥体（EntryPoint.cs 源码常量原文：
// `Global\BCU-singleinstance`，含 Global 前缀跨会话可见）。
const mutexName = `Global\BCU-singleinstance`

type windowsBCUProbe struct{}

// NewBCUProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewBCUProbe() BCUProbe { return &windowsBCUProbe{} }

func (p *windowsBCUProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsBCUProbe) WaitForReady(timeout time.Duration) bool {
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

// IsMainWindowOpen 指定进程是否有可见顶层窗口。
// WinForms 窗口类名不可预测，只能按 PID 枚举顶层窗口判断（EnumWindows）。
func (p *windowsBCUProbe) IsMainWindowOpen(pid uint32) bool {
	return hasVisibleWindowByPID(pid)
}
