//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

// mutexName 上游单实例互斥体名（Common/constants.hpp MUTEX_GUID，main.cpp 以
// CreateMutexW 裸名创建 = Local 命名空间；2026.2 便携 exe 二进制字符串实证）。
// 跨版本恒定：GUID 字面量写死在源码，非包名派生。
const mutexName = "344635E9-9AE4-4E60-B128-D53E25AB70A7"

type windowsTBProbe struct{}

// NewTBProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewTBProbe() TBProbe { return &windowsTBProbe{} }

func (p *windowsTBProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsTBProbe) WaitForReady(timeout time.Duration) bool {
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
