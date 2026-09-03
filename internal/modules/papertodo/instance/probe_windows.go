//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

// mutexName 上游 SingleInstanceHelper 的裸互斥体名（"PaperTodo-SingleInstance-Mutex"，
// 源码实证 new Mutex(true, name)——无 Global\ 前缀即会话 Local 命名空间，
// 同用户同会话的 Hanxi 以同名 OpenMutex 可见）。
const mutexName = "PaperTodo-SingleInstance-Mutex"

type windowsPaperProbe struct{}

// NewPaperProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewPaperProbe() PaperProbe { return &windowsPaperProbe{} }

func (p *windowsPaperProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsPaperProbe) WaitForReady(timeout time.Duration) bool {
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
