//go:build windows

package instance

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsProbe piik 探针 Windows 实现：进程存在性用 CreateToolhelp32Snapshot
// 枚举 piik-app.exe（上游无单实例 mutex——8787 端口绑定即事实互斥，进程名
// 是唯一稳定标识，ddnsgo/flclash 同款策略）；端口就绪用回环 TCP 拨测。
type windowsProbe struct {
	netPortProbe
}

// NewProbe 构造平台探针（service 层注入；引擎与 service 只依赖 Probe 接口，
// 单测注入假探针，零真进程）。
func NewProbe() Probe { return &windowsProbe{} }

// FindPIDs 枚举全部 piik-app.exe 进程 PID（含外部实例；同名即计入，
// runtime 双 exe 孙进程异名不混入）。快照句柄失败按空集处理（探测永不报错，
// 任何失败即"不存在"，与引擎瞬时探测纪律一致）。
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
		if strings.EqualFold(name, exeImageName) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}

func (p *windowsProbe) ProcessRunning() bool {
	return len(p.FindPIDs()) > 0
}
