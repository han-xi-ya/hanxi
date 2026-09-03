//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName LiteMonitor 主进程名（官方 zip 内 exe 名一致）。
const exeName = "LiteMonitor.exe"

type windowsLiteMonitorProbe struct{}

// NewLiteMonitorProbe Windows 实现：进程快照枚举探测。
// 上游单实例互斥体名随安装路径派生（见包注释），外部实例无法预测名称，
// 进程名是唯一稳定标识——与 FlClash 同策略，放弃 OpenMutex。
func NewLiteMonitorProbe() LiteMonitorProbe { return &windowsLiteMonitorProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 LiteMonitor.exe 进程 PID。
func (p *windowsLiteMonitorProbe) FindPIDs() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var pids []uint32
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, exeName) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}

func (p *windowsLiteMonitorProbe) IsRunning() bool {
	return len(p.FindPIDs()) > 0
}

func (p *windowsLiteMonitorProbe) WaitForReady(timeout time.Duration) bool {
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

// IsMainWindowOpen 这些进程中是否有可见顶层窗口（EnumWindows 按 PID 集合匹配）。
// LiteMonitor 主窗为 TopMost 横条；HideMainForm/边缘自动隐藏时不可见。
func (p *windowsLiteMonitorProbe) IsMainWindowOpen(pids []uint32) bool {
	if len(pids) == 0 {
		return false
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return hasVisibleWindowByPIDs(set)
}
