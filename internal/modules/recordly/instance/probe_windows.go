//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	win "hanxi/internal/platform/windows"
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

// IsMainWindowOpen 存在归属 Recordly.exe 进程、可见且带标题的顶层窗口
// （无标题窗口不作为"主窗口在场"证据——判据即平台公共件口径）。
// Electron 无固定窗口类名（类名是 Chromium 通用 Chrome_WidgetWin_1，
// 不能当身份用），只能顶层窗口枚举 + 进程名过滤。旧写法每次调用
// syscall.NewCallback 现场注册闭包烧回调槽（池上限 2000、永不回收），
// 现委托公共件静态回调枚举；本包 WM_CLOSE 投递仍是自实现枚举（close_windows.go）。
func (p *windowsRecordlyProbe) IsMainWindowOpen() bool {
	pids := recordlyPIDs()
	if len(pids) == 0 {
		return false // 无进程在场免枚举（WaitForReady 轮询快路径）
	}
	return win.HasFocusableTopWindowForPIDs(toWinPIDSet(pids))
}

// toWinPIDSet 把包内 map[uint32]bool 的 PID 集合转成平台公共件的集合形状。
func toWinPIDSet(pids map[uint32]bool) map[uint32]struct{} {
	set := make(map[uint32]struct{}, len(pids))
	for pid := range pids {
		set[pid] = struct{}{}
	}
	return set
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
