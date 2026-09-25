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

// 进程名匹配按 payloadImageName()（本架构 RAMMap64.exe / RAMMap64a.exe）；
// 官方 zip 另两架构 exe 不并跑，锚单一名即可判实例在场。

// 下列 user32 句柄仅供本包 WM_CLOSE 投递（close_windows.go）使用；
// 窗口枚举/唤窗已全部委托平台公共件（winfocus），旧 syscall.NewCallback
// 每次调用烧一回调槽的定时炸弹随之根除。
var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procIsWinVisible     = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextLen = modUser32.NewProc("GetWindowTextLengthW")
)

// rammapProbe 进程名快照 + 顶层窗口枚举探针（契约见 prober.go；
// guoheview 同族手法，Inspect 按 snipaste 规格充实身份供分档退出取令牌）。
type rammapProbe struct {
	proc platform.ProcessAPI
}

// NewRAMMapProbe Windows 探针构造（service 层注入 platform.ProcessAPI）。
func NewRAMMapProbe(proc platform.ProcessAPI) Probe {
	return &rammapProbe{proc: proc}
}

func (p *rammapProbe) Inspect(_ context.Context, ownPID uint32) (bool, *platform.ProcInfo, error) {
	found := false
	for pid := range rammapPIDs() {
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

// WaitForReady 轮询"可见 + 带标题"顶层窗口出现（GUI 程序窗口即就绪；
// 启动期隐藏宿主窗口/IME 附属窗口无标题，天然被过滤）。
func (p *rammapProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.anyVisibleWindow(rammapPIDs()) {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.anyVisibleWindow(rammapPIDs())
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (p *rammapProbe) FocusMainWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return p.focusInPids(map[uint32]bool{pid: true})
}

func (p *rammapProbe) FocusAnyWindow() bool {
	return p.focusInPids(rammapPIDs())
}

// anyVisibleWindow pids 集合内是否存在可见且带标题的顶层窗口（判据即平台
// 公共件口径；启动期隐藏宿主窗口/IME 附属窗口无标题，天然被过滤）。
func (p *rammapProbe) anyVisibleWindow(pids map[uint32]bool) bool {
	if len(pids) == 0 {
		return false
	}
	return win.HasFocusableTopWindowForPIDs(toWinPIDSet(pids))
}

// focusInPids 唤回给定 PID 集合中第一个可聚焦顶层窗——委托平台公共件
// （可见+标题过滤、IsIconic 才 SW_RESTORE、SetForegroundForce 借权置前、
// SwitchToThisWindow 兜底；guoheview 标杆形态的收口结果）。多实例语义下
// 按 PID 集合收敛，绝不按窗口标题广播。
func (p *rammapProbe) focusInPids(pids map[uint32]bool) bool {
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

// rammapPIDs 当前所有 RAMMap 进程 PID 集合（Toolhelp32 快照按名
// 匹配；不查完整路径，避免跨权限场景 QueryFullProcessImageName 失败造成漏报）。
func rammapPIDs() map[uint32]bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	out := map[uint32]bool{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), payloadImageName()) {
			out[entry.ProcessID] = true
		}
	}
	return out
}
