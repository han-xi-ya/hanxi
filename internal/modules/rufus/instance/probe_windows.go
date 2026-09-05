//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeName 托管落盘定名（版本隔离目录内恒为此名）。
const exeName = "rufus.exe"

// mutexName 上游单实例互斥体（src/rufus.c：CreateMutexA(NULL, TRUE,
// "Global/" APPLICATION_NAME)，APPLICATION_NAME="Rufus"）。
// 对象命名空间分隔符 "/" 与 "\" 等价，此处按内核路径规范用反斜杠。
const mutexName = `Global\Rufus`

type windowsRufusProbe struct{}

// NewRufusProbe Windows 实现：互斥体存在性 + 进程快照枚举混合探测。
func NewRufusProbe() RufusProbe { return &windowsRufusProbe{} }

// IsRunning 先查 Global\Rufus 互斥体（固定名、不随 exe 改名漂移，最小权限
// 只申请 SYNCHRONIZE），再退到进程枚举（防异常环境下互斥体探测失灵的兜底）。
func (p *windowsRufusProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err == nil {
		_ = windows.CloseHandle(h)
		return true
	}
	return len(p.FindPIDs()) > 0
}

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 Rufus 进程 PID。
// 外部实例常以浏览器下载原名（rufus-4.15p.exe）运行，故按形态族匹配。
func (p *windowsRufusProbe) FindPIDs() []uint32 {
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
		if isRufusProcess(name) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}

func (p *windowsRufusProbe) WaitForReady(timeout time.Duration) bool {
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
// Rufus 主界面是模态对话框（类 #32770），顶层枚举天然覆盖。
func (p *windowsRufusProbe) IsMainWindowOpen(pids []uint32) bool {
	if len(pids) == 0 {
		return false
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		set[pid] = struct{}{}
	}
	return hasVisibleWindowByPIDs(set)
}

// isRufusProcess 进程名形态判定：托管定名 rufus.exe，或官方资产形态
// rufus-<版本>p.exe / rufus-<版本>.exe（前缀 "rufus-" + ".exe" 后缀）。
// 上游"cmdline hogger"落盘名 rufus.com 不在 .exe 族内，天然排除。
func isRufusProcess(name string) bool {
	lower := strings.ToLower(name)
	if lower == exeName {
		return true
	}
	return strings.HasPrefix(lower, "rufus-") && strings.HasSuffix(lower, ".exe")
}
