package instance

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeNamePrefix 主进程名前缀（不含扩展名，大小写不敏感）。
//
// 上游文件名随版本漂移（实测 HEU_KMS_Activator_v64.0.0.exe），故判据取前缀而非
// 精确名。**约束**：若用户把主程序改名（如 heu.exe），进程探测与外部实例感知都会
// 落空——service 层的导入指引如实写明"保留上游原始文件名"，不做静默宽容。
const exeNamePrefix = "HEU_KMS_Activator"

type windowsHeuProbe struct{}

// NewHeuProbe Windows 实现：进程快照枚举探测（放弃 OpenMutex，理由见包注释）。
func NewHeuProbe() HeuProbe { return &windowsHeuProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 HEU 主程序进程 PID。
func (p *windowsHeuProbe) FindPIDs() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var pids []uint32
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		// 比较去扩展名：32 位程序在快照里仍是 .exe 全名，去扩展后与"文件名漂移"
		// 的两种形态（带版本串 / 用户改名后仍留前缀）都能对齐。
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.HasPrefix(strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))), strings.ToLower(exeNamePrefix)) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}

func (p *windowsHeuProbe) IsRunning() bool { return len(p.FindPIDs()) > 0 }

func (p *windowsHeuProbe) WaitForReady(timeout time.Duration) bool {
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

// IsMainWindowOpen 这些进程中是否有可唤顶层窗口（含最小化在场）。
// HEU 是普通窗口程序，启动初期窗口尚未创建时返回 false——service 层的
// readyWait 与前端"进程在但窗口未见"提示共用这一判据。
func (p *windowsHeuProbe) IsMainWindowOpen(pids []uint32) bool {
	if len(pids) == 0 {
		return false
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return hasVisibleWindowByPIDs(set)
}
