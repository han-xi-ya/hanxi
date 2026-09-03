//go:build windows

package instance

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeImageName Recordly 打包主程序进程名（electron-builder executableName 固定）。
// Electron 是进程树模型（主进程 + 渲染/GPU/采集 helper 同名），本文件所有探测
// 都以"存在任一 Recordly.exe 顶层可见窗口"为实例可用信号——按窗口而非进程
// 计数，天然免疫 helper 残留。
const exeImageName = "Recordly.exe"

type windowsRecordlyProbe struct{}

// NewRecordlyProbe Windows 实现：进程名 + 顶层窗口联合探测（详见 prober.go 契约说明）。
func NewRecordlyProbe() RecordlyProbe { return &windowsRecordlyProbe{} }

func (p *windowsRecordlyProbe) IsRunning() bool {
	return anyRecordlyProcess()
}

func (p *windowsRecordlyProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsMainWindowOpen() {
			return true
		}
		if time.Now().After(deadline) {
			return p.IsMainWindowOpen() // 末位复探，覆盖探测与超时判断的边界竞态
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// IsMainWindowOpen 存在归属 Recordly.exe 进程、可见且带标题的顶层窗口。
// Electron 无固定窗口类名（类名是 Chromium 通用 Chrome_WidgetWin_1，
// 不能当身份用），只能 EnumWindows + 进程名过滤；要求标题非空以排除
// 启动早期的隐藏宿主窗口与无题辅助窗口。
func (p *windowsRecordlyProbe) IsMainWindowOpen() bool {
	pids := recordlyPIDs()
	if len(pids) == 0 {
		return false
	}
	found := false
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWinVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLen.Call(hwnd, 0, 0); length == 0 {
			return 1 // 无标题窗口不作为"主窗口在场"证据
		}
		var wpid uint32
		if r, _, _ := procGetWndThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid))); r != 0 && pids[wpid] {
			found = true
			return 0
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return found
}

// anyRecordlyProcess Toolhelp32 快照按进程名匹配（不查路径，避免跨用户
// 权限下 QueryFullProcessImageName 失败造成漏报）。
func anyRecordlyProcess() bool {
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

// recordlyPIDs 当前所有 Recordly.exe 进程 PID 集合。
func recordlyPIDs() map[uint32]bool {
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
