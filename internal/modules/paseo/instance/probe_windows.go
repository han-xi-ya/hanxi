//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeImageName Paseo 打包主程序进程名（electron-builder executableName 固定）。
// Electron 是进程树模型（主进程 + 渲染/GPU helper 同名；内置 daemon 走
// ELECTRON_RUN_AS_NODE 亦复用同一镜像），本文件探测以"存在任一 Paseo.exe
// 顶层可见窗口 / 进程在场"为信号——按窗口而非进程计数，天然免疫 helper 残留。
const exeImageName = "Paseo.exe"

type windowsPaseoProbe struct{}

// NewPaseoProbe Windows 实现：进程名 + 顶层窗口联合探测（详见 prober.go 契约说明）。
func NewPaseoProbe() PaseoProbe { return &windowsPaseoProbe{} }

func (p *windowsPaseoProbe) IsRunning() bool {
	return anyPaseoProcess()
}

func (p *windowsPaseoProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsWindowOpen() {
			return true
		}
		if time.Now().After(deadline) {
			return p.IsWindowOpen() // 末位复探，覆盖探测与超时判断的边界竞态
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (p *windowsPaseoProbe) IsWindowOpen() bool {
	open := false
	forEachPaseoWindow(func(uintptr) bool {
		open = true
		return false // 找到即可停止枚举
	})
	return open
}

// FocusWindow 恢复并置前台首个可见带标题的 Paseo 窗口（Win32 直操作，
// 与 litemonitor 的 restoreWindowByPID 同族）。SetForegroundWindow 受前台
// 锁限制时窗口至少被 ShowWindow 恢复——返回值表达"唤起了窗口"尽力语义，
// 真机验收含前台性核对。
func (p *windowsPaseoProbe) FocusWindow() bool {
	focused := false
	forEachPaseoWindow(func(hwnd uintptr) bool {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		focused = true
		return false
	})
	return focused
}

// anyPaseoProcess Toolhelp32 快照按进程名匹配（不查路径，避免跨用户
// 权限下 QueryFullProcessImageName 失败造成漏报；recordly 同纪律）。
func anyPaseoProcess() bool {
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

// paseoPIDs 当前所有 Paseo.exe 进程 PID 集合。
func paseoPIDs() map[uint32]bool {
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
