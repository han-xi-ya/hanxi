//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"
)

// mutexName tauri-plugin-single-instance 的 Windows 互斥体命名：
// {identifier}-sim（identifier 来自 tauri.conf.json = "org.keyviz"，插件未启用
// semver 特性则跨版本恒定）。插件在应用启动即 CreateMutexW 持有，互斥体存在 =
// 主实例存活——与 ccswitch/piclite 同款已真机验证语义，本模块另经一次性
// OpenMutex 实测确认名称命中（侦查阶段拉起 extracted v2.1.1 实测 FOUND）。
const mutexName = "org.keyviz-sim"

type windowsKeyvizProbe struct{}

// NewKeyvizProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewKeyvizProbe() KeyvizProbe { return &windowsKeyvizProbe{} }

func (p *windowsKeyvizProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsKeyvizProbe) WaitForReady(timeout time.Duration) bool {
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
