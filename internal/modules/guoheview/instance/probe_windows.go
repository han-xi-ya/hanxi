//go:build windows

package instance

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeImageName 果核看图主程序进程名（便携 zip 有效载荷固定，实测 3.2.7）。
// 上游是多实例应用（真机实证无单实例互斥体），本文件所有探测以
// "进程名 GuoheView.exe + EnumWindows 归属过滤"为身份判据；窗口类
// UiCore_Window 为果核 core-ui 框架共享类名，严禁用作 FindWindow 条件。
const exeImageName = "GuoheView.exe"

var (
	modUser32                 = syscall.NewLazyDLL("user32.dll")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWndThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWinVisible          = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen      = modUser32.NewProc("GetWindowTextLengthW")
	procIsIconic              = modUser32.NewProc("IsIconic")
	procShowWindow            = modUser32.NewProc("ShowWindow")
	procSwitchToThisWindow    = modUser32.NewProc("SwitchToThisWindow")
	procPostMsg               = modUser32.NewProc("PostMessageW")
)

const (
	swRestore = 9 // SW_RESTORE：最小化恢复
	// 标题非空作为"用户面窗口"证据：启动期隐藏宿主窗口/IME 附属窗口无标题，
	// 设置类子面板（如"图片信息 - GuoheView"）恒不可见——三者都被过滤。
)

type windowsViewProbe struct{}

// NewViewProbe Windows 实现：进程名快照 + 顶层窗口枚举（契约见 prober.go）。
func NewViewProbe() ViewProbe { return &windowsViewProbe{} }

func (p *windowsViewProbe) IsRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeImageName) {
			return true
		}
	}
	return false
}

func (p *windowsViewProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.anyVisibleWindow(viewPIDs()) {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.anyVisibleWindow(viewPIDs())
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// anyVisibleWindow pids 集合内是否存在可见且带标题的顶层窗口。
func (p *windowsViewProbe) anyVisibleWindow(pids map[uint32]bool) bool {
	if len(pids) == 0 {
		return false
	}
	found := false
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return 1
		}
		var wpid uint32
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && pids[wpid] {
			found = true
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// FocusMainWindow 按自有 PID 找第一个可见带标题窗口：IsIconic 则 SW_RESTORE，
// 随后 SwitchToThisWindow（系统为"用户主动唤回窗口"设计的旧版 API，
// 不受 SetForegroundWindow 前台锁限制，恢复+置顶+聚焦一步完成）。
// 多实例语义下只碰自有 PID 的窗口，用户自行打开的其他看图窗口不被打扰。
func (p *windowsViewProbe) FocusMainWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return p.focusInPids(map[uint32]bool{pid: true})
}

func (p *windowsViewProbe) FocusAnyWindow() bool {
	return p.focusInPids(viewPIDs())
}

// focusInPids 在给定 PID 集合中唤回第一个可见带标题顶层窗口：
// IsIconic 则 SW_RESTORE，随后 SwitchToThisWindow（系统为"用户主动唤回窗口"
// 设计的旧版 API，不受 SetForegroundWindow 前台锁限制，恢复+置顶+聚焦一步完成）。
func (p *windowsViewProbe) focusInPids(pids map[uint32]bool) bool {
	if len(pids) == 0 {
		return false
	}
	focused := false
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if focused {
			return 0
		}
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return 1
		}
		var wpid uint32
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r == 0 || !pids[wpid] {
			return 1
		}
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
			procShowWindow.Call(hwnd, swRestore)
		}
		procSwitchToThisWindow.Call(hwnd, 1)
		focused = true
		return 0
	})
	procEnumWindows.Call(cb, 0)
	return focused
}

// viewPIDs 当前所有 GuoheView.exe 进程 PID 集合（Toolhelp32 快照按名匹配；
// 不查完整路径，避免跨权限场景 QueryFullProcessImageName 失败造成漏报）。
func viewPIDs() map[uint32]bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	out := map[uint32]bool{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeImageName) {
			out[entry.ProcessID] = true
		}
	}
	return out
}
