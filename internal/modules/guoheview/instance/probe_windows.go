//go:build windows

package instance

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	win "hanxi/internal/platform/windows"
)

// exeImageName 果核看图主程序进程名（便携 zip 有效载荷固定，实测 3.2.7）。
// 上游是多实例应用（真机实证无单实例互斥体），本文件所有探测以
// "进程名 GuoheView.exe + EnumWindows 归属过滤"为身份判据；窗口类
// UiCore_Window 为果核 core-ui 框架共享类名，严禁用作 FindWindow 条件。
const exeImageName = "GuoheView.exe"

// 下列 user32 句柄仅供本包 WM_CLOSE 投递（close_windows.go）使用；
// 窗口枚举/唤窗已全部委托平台公共件（winfocus），旧 syscall.NewCallback
// 每次调用烧一回调槽的定时炸弹随之根除。
var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procIsWinVisible     = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen = modUser32.NewProc("GetWindowTextLengthW")
	procPostMsg          = modUser32.NewProc("PostMessageW")
)

type windowsViewProbe struct{}

// NewViewProbe Windows 实现：进程名快照 + 顶层窗口枚举（契约见 prober.go）。
func NewViewProbe() ViewProbe { return &windowsViewProbe{} }

func (p *windowsViewProbe) IsRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeImageName) {
			return true
		}
	}
	return false
}

// RunningBesides 外部/自有归属判定按探针 PID：枚举所有 GuoheView.exe，
// 存在 PID ≠ ownPID 者即"有非自有实例在场"（ownPID=0 时任何进程都算，
// 等价于 IsRunning）。多实例上游的实例计数账目不在此收口（内核单 ProcInfo
// 公理表达不了计数，计数留模块，见 instance.go 的 supProbe 注释）。
func (p *windowsViewProbe) RunningBesides(ownPID uint32) bool {
	for pid := range viewPIDs() {
		if pid != ownPID {
			return true
		}
	}
	return false
}

func (p *windowsViewProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.anyVisibleWindow(viewPIDs()) {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.anyVisibleWindow(viewPIDs())
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// anyVisibleWindow pids 集合内是否存在可见且带标题的顶层窗口（判据即平台
// 公共件口径：标题非空作为"用户面窗口"证据——启动期隐藏宿主窗口/IME 附属
// 窗口无标题，设置类子面板（如"图片信息 - GuoheView"）恒不可见，三者都被过滤）。
func (p *windowsViewProbe) anyVisibleWindow(pids map[uint32]bool) bool {
	if len(pids) == 0 {
		return false
	}
	return win.HasFocusableTopWindowForPIDs(toWinPIDSet(pids))
}

// FocusMainWindow 按自有 PID 唤回第一个可聚焦顶层窗（委托平台公共件：
// 可见+标题过滤、IsIconic 才 SW_RESTORE、SetForegroundForce 借权置前、
// SwitchToThisWindow 兜底——本包曾自实现的 SwitchToThisWindow 形态即公共件
// 收口的标杆原型，现直接复用收口结果）。
// 多实例语义下只碰自有 PID 的窗口，用户自行打开的其他看图窗口不被打扰。
func (p *windowsViewProbe) FocusMainWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return p.focusInPids(map[uint32]bool{pid: true})
}

func (p *windowsViewProbe) FocusAnyWindow() bool {
	return p.focusInPids(viewPIDs())
}

// focusInPids 在给定 PID 集合中唤回第一个可聚焦顶层窗（判据与动作三要素
// 见上；枚举经公共件静态回调，零回调槽消耗）。
func (p *windowsViewProbe) focusInPids(pids map[uint32]bool) bool {
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

// viewPIDs 当前所有 GuoheView.exe 进程 PID 集合（Toolhelp32 快照按名匹配；
// 不查完整路径，避免跨权限场景 QueryFullProcessImageName 失败造成漏报）。
func viewPIDs() map[uint32]bool {
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
