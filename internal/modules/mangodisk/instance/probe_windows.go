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

type windowsMangoDiskProbe struct{}

func NewMangoDiskProbe() MangoDiskProbe { return &windowsMangoDiskProbe{} }

func (p *windowsMangoDiskProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

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
