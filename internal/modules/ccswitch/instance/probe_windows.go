//go:build windows

package instance

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mutexName tauri-plugin-single-instance 的 Windows 互斥体命名：
// {identifier}-sim（identifier 来自 tauri.conf.json = "com.ccswitch.desktop"，无 semver 特性
// 则跨版本恒定）。插件在应用启动即 CreateMutexW 持有，互斥体存在 = 主实例存活——
// 经真机验证过的 markeron 同款语义。窗口类/窗口名（-sic/-siw）同源，供 WM_CLOSE 信使定位。
const mutexName = "com.ccswitch.desktop-sim"

type windowsCCProbe struct{}

// NewCCProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewCCProbe() CCSwitchProbe { return &windowsCCProbe{} }

func (p *windowsCCProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsCCProbe) WaitForReady(timeout time.Duration) bool {
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

// IsMainWindowOpen 主窗口是否可见：FindWindowW（-sic 类名）命中且 IsWindowVisible。
// 关窗驻托盘时窗口仅被 hide（FindWindowW 仍可命中但不可见）——
// 恰是"无人操作 3 分钟即退出"想要覆盖的场。
func (p *windowsCCProbe) IsMainWindowOpen() bool {
	cls, _ := syscall.UTF16PtrFromString(classCCSwitch)
	name, _ := syscall.UTF16PtrFromString(nameCCSwitch)
	hwnd, _, _ := procFindWnd.Call(
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(name)),
	)
	if hwnd == 0 {
		return false
	}
	visible, _, _ := procIsWinVisible.Call(hwnd)
	return visible != 0
}
