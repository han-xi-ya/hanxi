//go:build windows

package instance

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32      = syscall.NewLazyDLL("user32.dll")
	procFindWindow = modUser32.NewProc("FindWindowW")
)

type windowsEverythingProbe struct{}

// NewEverythingProbe Windows 实现：双通道探测。
//  1. FindWindowW 查托盘通知窗口类（ES.exe 同一 IPC 通道，最权威）；
//  2. OpenMutex 查默认命名实例互斥体（兜底窗口类未来改名的情况）。
//
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，避免特殊 ACL 误判。
func NewEverythingProbe() EverythingProbe { return &windowsEverythingProbe{} }

func (p *windowsEverythingProbe) IsEverythingRunning() bool {
	cls, err := syscall.UTF16PtrFromString(WindowClass)
	if err == nil {
		r, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
		if r != 0 {
			return true
		}
	}
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(MutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsEverythingProbe) WaitForEverythingReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsEverythingRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsEverythingRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// IsSearchWindowOpen 搜索主窗口存在性（FindWindowW 类名探测，与托盘窗口类无关）。
func (p *windowsEverythingProbe) IsSearchWindowOpen() bool {
	cls, err := syscall.UTF16PtrFromString(SearchWindowClass)
	if err != nil {
		return false
	}
	r, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
	return r != 0
}
