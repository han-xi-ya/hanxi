//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName AirSnip 主进程名（官方 zip 内 exe 名一致，导入件同名）。
const exeName = "airsnip.exe"

type windowsAirsnipProbe struct{}

// NewAirsnipProbe Windows 实现：进程快照枚举探测。
// 上游虽有命名互斥体 Local\airsnip.single.instance，但 release 版以
// requireAdministrator 运行、对象默认 ACL 随提权令牌派生，中完整性 Hanxi 的
// OpenMutex 放行与否无实证（见包注释）；进程名快照对完整性差异免疫且能带出
// 存活 PID——与 litemonitor/snipaste 同策略，放弃 OpenMutex。
func NewAirsnipProbe() AirsnipProbe { return &windowsAirsnipProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 airsnip.exe 进程 PID。
func (p *windowsAirsnipProbe) FindPIDs() []uint32 {
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

func (p *windowsAirsnipProbe) IsRunning() bool { return len(p.FindPIDs()) > 0 }

func (p *windowsAirsnipProbe) WaitForReady(timeout time.Duration) bool {
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
