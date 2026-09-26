//go:build windows

package instance

import (
	"errors"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 单实例探针领域常量（阶段 0 实证，identifier = com.dbx.app）：
//   - 互斥体 {identifier}-sim：tauri-plugin-single-instance 应用侧以
//     CreateMutexW 持有、存活期间恒在（==ERROR_ALREADY_EXISTS 即让位转发，
//     是上游自己的存活判据）；semver feature 未启用 → 无版本后缀、跨版本恒定。
//     探测侧用 OpenMutex(SYNCHRONIZE) 最小权限只读复核（ccswitch 同款纪律，
//     避免 MUTEX_ALL_ACCESS 在特殊 ACL 场误拒）。
//   - 隐藏消息窗 类 {identifier}-sic / 名 {identifier}-siw：插件的 WM_COPYDATA
//     转发窗，常驻在场且不可见——**禁止充当主窗判据**（拿它探活/就绪会把
//     状态机永久钉在"运行"）。主窗判据只用标题 "DBX" 的可见顶层窗。
const (
	mutexName       = "com.dbx.app-sim"
	msgWindowClass  = "com.dbx.app-sic"
	msgWindowName   = "com.dbx.app-siw"
	exeImageName    = "DBX.exe"
	mainWindowTitle = "DBX"
)

// mutexVerdict 互斥体探测三分裁决：held=主实例持有在场；absent=确证不在场
// （OpenMutex 得 ERROR_FILE_NOT_FOUND）；refused=拒探（ACL/完整性拦截，
// 对象存在与否不可从本进程下结论）。
type mutexVerdict int

const (
	mutexAbsent mutexVerdict = iota
	mutexHeld
	mutexRefused
)

// windowsDBXProbe 互斥体主判据 + 拒探分治兜底（进程名 DBX.exe 枚举 +
// EnumWindows 标题 "DBX" 主窗判据）。三个判据均为可注入接缝：
// 生产装配真实 Win32 实现，单测注入造景以钉死分治决策表。
type windowsDBXProbe struct {
	mutex        func() mutexVerdict // 互斥体三分探测
	findPIDs     func() []uint32     // 进程名枚举（含外部实例/备份 worker）
	mainWindowUp func(pids []uint32) bool
}

// NewDBXProbe Windows 实现。
func NewDBXProbe() DBXProbe {
	return &windowsDBXProbe{
		mutex:        queryMutex,
		findPIDs:     findDBXPIDs,
		mainWindowUp: hasTitleWindowByPIDs,
	}
}

// queryMutex OpenMutex(SYNCHRONIZE) 三分裁决：句柄到手=held；
// ERROR_FILE_NOT_FOUND=absent（确证不在场）；ERROR_ACCESS_DENIED 及其他
// 意外错误=refused（拒探——存在性可疑但无从取证，交给进程/主窗兜底分治）。
func queryMutex() mutexVerdict {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	switch {
	case err == nil:
		_ = windows.CloseHandle(h)
		return mutexHeld
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
		return mutexAbsent
	default:
		// ERROR_ACCESS_DENIED（他主高完整性实例）与一切意外错误同桶保守处理
		return mutexRefused
	}
}

// IsRunning 存活判定分治（阶段 0 纪律）：互斥体结论可得的场以互斥体为准
// （单实例插件持有 = 主实例存活，信使短命进程天然不惊扰判据）；拒探场
// （elevated 外部实例等）兜底进程名——托管备份 worker 残留（脱出 Job 的
// --managed-backup-worker 进程）经此通道被 Quit 后的 RefreshExternal 如实
// 甄别为 external。
func (p *windowsDBXProbe) IsRunning() bool {
	switch p.mutex() {
	case mutexHeld:
		return true
	case mutexAbsent:
		return false
	default: // mutexRefused：拒探兜底
		return len(p.findPIDs()) > 0
	}
}

// WaitForReady 轮询等待就绪：互斥体在场 = 就绪（主实例完成插件初始化，
// ccswitch 同判据）；拒探场退而求其次——标题 "DBX" 的可见主窗在场才算就绪
// （进程在场 ≠ 可交互，WebView2 初始化完成才有主窗）。
func (p *windowsDBXProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.ready() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.ready()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ready 单次就绪判据（mutexHeld 主通道；mutexRefused 走主窗兜底；
// mutexAbsent 一律未就绪）。
func (p *windowsDBXProbe) ready() bool {
	switch p.mutex() {
	case mutexHeld:
		return true
	case mutexAbsent:
		return false
	default:
		return p.mainWindowUp(p.findPIDs())
	}
}

// IsMainWindowOpen 是否存在可见且标题为 "DBX" 的 DBX.exe 顶层窗
// （判据实现在 close_windows.go；隐藏消息窗 -sic/-siw 不在判据面内）。
func (p *windowsDBXProbe) IsMainWindowOpen() bool {
	return p.mainWindowUp(p.findPIDs())
}

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 DBX.exe 进程 PID。
// 刻意不查路径（QueryFullProcessImageName 跨用户权限下会失败造成漏报），
// 进程名是唯一稳定标识——litemonitor/gonavi 同口径。WebView2 子进程为
// msedgewebview2.exe 异名，不会污染计数。
func (p *windowsDBXProbe) FindPIDs() []uint32 {
	return p.findPIDs()
}

func findDBXPIDs() []uint32 {
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
