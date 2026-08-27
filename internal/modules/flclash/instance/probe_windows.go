//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName FlClash 主进程名（Flutter windows 构建产物名，与 zip 内 exe 一致）。
const exeName = "FlClash.exe"

type windowsFlClashProbe struct{}

// NewFlClashProbe Windows 实现：进程快照枚举探测（FlClash 单实例是文件锁，
// 没有命名互斥体可供 OpenMutex；进程名是唯一稳定标识）。
func NewFlClashProbe() FlClashProbe { return &windowsFlClashProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 FlClash.exe 进程 PID。
func (p *windowsFlClashProbe) FindPIDs() []uint32 {
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

func (p *windowsFlClashProbe) IsRunning() bool {
	return len(p.FindPIDs()) > 0
}

func (p *windowsFlClashProbe) WaitForReady(timeout time.Duration) bool {
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
func (p *windowsFlClashProbe) IsMainWindowOpen(pids []uint32) bool {
	if len(pids) == 0 {
		return false
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return hasVisibleWindowByPIDs(set)
}
