//go:build windows

package instance

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mutexName        = "app.mangodisk.desktop-sim"
	signalClassName  = "app.mangodisk.desktop-sic"
	signalWindowName = "app.mangodisk.desktop-siw"
)

var (
	probeUser32               = syscall.NewLazyDLL("user32.dll")
	procFindSignalWindow      = probeUser32.NewProc("FindWindowW")
	procSignalWindowProcessID = probeUser32.NewProc("GetWindowThreadProcessId")
)

// windowsMangoDiskProbe 通过命名对象探测 MangoDisk（Tauri 单实例机制）：
// mutexName 为主实例互斥体；signalClassName/signalWindowName 是其隐藏信号窗口，
// 窗口属主 PID 即主实例 PID。名称均为上游 MangoDisk 固定值，升级若变更需同步。
type windowsMangoDiskProbe struct{}

// NewMangoDiskProbe 返回 Windows 探测实现。
func NewMangoDiskProbe() MangoDiskProbe { return &windowsMangoDiskProbe{} }

// IsRunning 以 SYNCHRONIZE 权限只开不合互斥体判断存在性（不获取所有权，绝不干扰运行中实例）。
func (p *windowsMangoDiskProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

// SignalPID FindWindowW 按类名+窗口名定位隐藏信号窗口（进程内窗口，跨桌面也可见同会话），
// 再经 GetWindowThreadProcessId 取属主 PID；窗口不存在返回 (0, false)。
func (p *windowsMangoDiskProbe) SignalPID() (uint32, bool) {
	cls, _ := syscall.UTF16PtrFromString(signalClassName)
	name, _ := syscall.UTF16PtrFromString(signalWindowName)
	hwnd, _, _ := procFindSignalWindow.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		return 0, false
	}
	var pid uint32
	ret, _, _ := procSignalWindowProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid, ret != 0 && pid != 0
}

// WaitForReady 自旋轮询互斥体+信号窗口双条件（无事件可等，Tauri 不暴露就绪通知）。
// 超时前若互斥体消失判定启动失败；忙等周期见尾部 sleep，勿在热路径高频调用。
func (p *windowsMangoDiskProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsRunning() {
			if _, ok := p.SignalPID(); ok {
				return true
			}
		}
		if time.Now().After(deadline) {
			if !p.IsRunning() {
				return false
			}
			_, ok := p.SignalPID()
			return ok
		}
		time.Sleep(100 * time.Millisecond)
	}
}
