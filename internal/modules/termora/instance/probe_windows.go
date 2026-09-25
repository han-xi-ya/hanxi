//go:build windows

package instance

import (
	"context"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"

	win "hanxi/internal/platform/windows"
)

// exeImageName Termora 主启动器进程名（jpackage app-image 固定，实测
// 2.0.0-beta.16）。附属 exe（winpty-agent/cyglaunch/OpenConsole/restart4j
// 等 pty/重启件）刻意不列入探测面：只随主进程生灭，单名锚定实例。
const exeImageName = "Termora.exe"

// mutexName 单实例命名互斥体（ApplicationSingleton.kt：CreateMutex(null,
// false, "termora")，会话级）。存在性探测只申请 SYNCHRONIZE 最小权限
// （ccswitch 同法）——互斥体证明"有实例在世"，PID 与路径身份仍走进程名枚举。
const mutexName = "termora"

// 下列 user32 句柄仅供本包 WM_CLOSE 投递（close_windows.go）使用；
// 窗口枚举/唤窗已全部委托平台公共件（winfocus），旧 syscall.NewCallback
// 每次调用烧一回调槽的定时炸弹随之根除。
var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procIsWinVisible     = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen = modUser32.NewProc("GetWindowTextLengthW")
)

// windowsProbe 进程名快照 + 顶层窗口枚举探针（契约见 prober.go；
// Inspect 按 snipaste 规格充实身份供分档退出取令牌；就绪快判走单实例互斥体）。
type windowsProbe struct {
	proc platform.ProcessAPI
}

// NewTermoraProbe Windows 探针构造（service 层注入 platform.ProcessAPI）。
func NewTermoraProbe(proc platform.ProcessAPI) Probe {
	return &windowsProbe{proc: proc}
}

func (p *windowsProbe) Inspect(_ context.Context, ownPID uint32) (bool, *platform.ProcInfo, error) {
	found := false
	for pid := range termoraPIDs() {
		if pid == ownPID {
			continue // 自有托管进程（含退出瞬间快照仍列出自己的竞态）
		}
		found = true // 在场事实成立，即使身份查询全败
		info, err := p.proc.Query(pid)
		if err != nil {
			continue
		}
		return true, &info, nil
	}
	if found {
		return true, nil, nil // 在场但身份不明：判 external 存在、禁强杀
	}
	return false, nil, nil
}

// MutexHeld 以 OpenMutex(SYNCHRONIZE) 探测单实例互斥体（任何失败均视为
// 不存在——ERROR_FILE_NOT_FOUND 常态，特殊 ACL 下 access denied 也不误报在场）。
func (p *windowsProbe) MutexHeld() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

// WaitForReady 就绪判据取互斥体在位（ccswitch 同法）：Termora 启动即持锁、
// 窗口构建在其后（HiDPI 缩放/恢复场景首窗可见性判定易抖动）——锁不会说谎。
func (p *windowsProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.MutexHeld() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.MutexHeld()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (p *windowsProbe) FocusMainWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return p.focusInPids(map[uint32]bool{pid: true})
}

func (p *windowsProbe) FocusAnyWindow() bool {
	return p.focusInPids(termoraPIDs())
}

// focusInPids 唤回给定 PID 集合中第一个可聚焦顶层窗——委托平台公共件
// （可见+标题过滤、IsIconic 才 SW_RESTORE、SetForegroundForce 借权置前、
// SwitchToThisWindow 兜底；guoheview 标杆形态的收口结果）。
// 按 PID 集合收敛，绝不按窗口标题/类名广播。
func (p *windowsProbe) focusInPids(pids map[uint32]bool) bool {
	if len(pids) == 0 {
		return false
	}
	return win.FocusTopWindowForPIDs(toWinPIDSet(pids))
}

// toWinPIDSet 把包内 map[uint32]bool 的 PID 集合转成平台公共件的集合形状。
func toWinPIDSet(pids map[uint32]bool) map[uint32]struct{} {
	set := make(map[uint32]struct{}, len(pids))
	for pid := range pids {
		set[pid] = struct{}{}
	}
	return set
}

// termoraPIDs 当前所有 Termora.exe 进程 PID 集合（Toolhelp32 快照按名
// 匹配；不查完整路径，避免跨权限场景 QueryFullProcessImageName 失败造成漏报）。
func termoraPIDs() map[uint32]bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	out := map[uint32]bool{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeImageName) {
			out[entry.ProcessID] = true
		}
	}
	return out
}
