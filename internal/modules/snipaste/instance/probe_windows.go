//go:build windows

package instance

import (
	"context"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"
)

// exeName Snipaste 主进程名（桌面版/商店版同名，zip 便携版与导入版均为此名）。
const exeName = "Snipaste.exe"

// windowsProbe 进程快照枚举探针：CreateToolhelp32Snapshot 命中 Snipaste.exe
// （flclash/litemonitor 同族手法；Snipaste 无命名互斥体可 OpenMutex，进程名
// 是唯一稳定标识）。Query 充实路径与启动时刻，供 external 快照展示与
// KillVerified 身份复核。
type windowsProbe struct {
	proc platform.ProcessAPI
}

// NewSnipasteProbe 注入 platform.ProcessAPI 构造 Windows 探针。
func NewSnipasteProbe(proc platform.ProcessAPI) Probe {
	return &windowsProbe{proc: proc}
}

func (p *windowsProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	pids := p.findPIDs()
	if len(pids) == 0 {
		return false, nil, nil
	}
	// Snipaste 设计上单实例（二次启动转发让位），多 PID 属罕见竞态：
	// 取 Query 成功的首个即可，全部失败按"探针失败"上报（内核保守判无）。
	for _, pid := range pids {
		info, err := p.proc.Query(pid)
		if err != nil {
			continue
		}
		return true, &info, nil
	}
	return false, nil, nil
}

// findPIDs 枚举全部 Snipaste.exe 进程 PID。
func (p *windowsProbe) findPIDs() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var pids []uint32
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeName) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}
