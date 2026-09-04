//go:build windows

package instance

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName ddns-go 主进程名（官方 windows zip 内唯一 exe，flat 布局）。
const exeName = "ddns-go.exe"

type windowsProbe struct {
	netPortProbe
}

// NewProbe Windows 实现：进程快照枚举探测（ddns-go 无命名互斥体/无单实例锁，
// 进程名是唯一稳定标识——flclash 同款策略）；端口就绪用 TCP 拨测。
func NewProbe() Probe { return &windowsProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 ddns-go.exe 进程 PID。
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
