//go:build windows

package instance

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName hanxi-ocr 主进程名（私发组件目录内唯一 exe）。
const exeName = "hanxi-ocr.exe"

type windowsProbe struct {
	netPortProbe
}

// NewProbe Windows 实现：进程名快照枚举（hanxi-ocr 无互斥体/单实例锁，
// 进程名是唯一稳定标识，仅作崩溃分类辅助）；端口就绪与外部实例判别走
// TCP 拨测 + /api/status 契约探测。
func NewProbe() Probe { return &windowsProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 hanxi-ocr.exe 进程 PID。
func (p *windowsProbe) FindPIDs() []uint32 {
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

func (p *windowsProbe) IsRunning() bool {
	return len(p.FindPIDs()) > 0
}
