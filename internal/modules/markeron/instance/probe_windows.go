//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

type windowsMarkerProbe struct{}

// NewMarkerProbe Windows 实现：以 OpenMutex 探测命名互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewMarkerProbe() MarkerProbe { return &windowsMarkerProbe{} }

func (p *windowsMarkerProbe) IsMarkerOnRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(MutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsMarkerProbe) WaitForMarkerOnReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsMarkerOnRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsMarkerOnRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}
