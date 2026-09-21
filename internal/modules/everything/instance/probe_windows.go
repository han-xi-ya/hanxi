//go:build windows

package instance

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"
)

var (
	modUser32      = syscall.NewLazyDLL("user32.dll")
	procFindWindow = modUser32.NewProc("FindWindowW")
)

// exeName Everything 主进程名（便携/安装版同名；-instance 定制实例不改进程名）。
const exeName = "Everything.exe"

type windowsEverythingProbe struct {
	proc platform.ProcessAPI
}

// NewEverythingProbe Windows 实现：双通道存在性探测 + 进程名枚举归属补充。
//  1. FindWindowW 查托盘通知窗口类（ES.exe 同一 IPC 通道，最权威）；
//  2. OpenMutex 查默认命名实例互斥体（兜底窗口类未来改名的情况）；
//  3. FindInstance 经 CreateToolhelp32Snapshot 取实例 PID 并经 ProcessAPI 充实
//     路径/启动时刻（窗口类/互斥体通道拿不到 PID 的历史缺口，W2/N2 补齐）。
//
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，避免特殊 ACL 误判。
func NewEverythingProbe(proc platform.ProcessAPI) EverythingProbe {
	return &windowsEverythingProbe{proc: proc}
}

func (p *windowsEverythingProbe) IsEverythingRunning() bool {
	cls, err := syscall.UTF16PtrFromString(WindowClass)
	if err == nil {
		r, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
		if r != 0 {
			return true
		}
	}
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(MutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsEverythingProbe) WaitForEverythingReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsEverythingRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsEverythingRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// IsSearchWindowOpen 搜索主窗口存在性（FindWindowW 类名探测，与托盘窗口类无关）。
func (p *windowsEverythingProbe) IsSearchWindowOpen() bool {
	cls, err := syscall.UTF16PtrFromString(SearchWindowClass)
	if err != nil {
		return false
	}
	r, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
	return r != 0
}

// FindInstance 进程名快照枚举 Everything.exe，Query 充实路径/启动时刻。
// Everything 单实例（互斥体协议），多命中属罕见竞态：取首个查询成功的；
// 全部查询失败（句柄权限受限等）返回 nil——"在运行"判定不受影响，仅缺归属细节。
func (p *windowsEverythingProbe) FindInstance() *platform.ProcInfo {
	pids := p.findPIDs()
	for _, pid := range pids {
		info, err := p.proc.Query(pid)
		if err != nil {
			continue
		}
		return &info
	}
	return nil
}

func (p *windowsEverythingProbe) findPIDs() []uint32 {
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
