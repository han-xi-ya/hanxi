//go:build windows

package instance

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// exeImageName GoNavi 主程序进程名（阶段 0 实证；zip 便携版与 MSI 安装版同名）。
// WebView2 子进程为 msedgewebview2.exe（异名），不会污染进程名计数——本探测
// 天然是"单主进程"形态，无需 recordly（Electron 同名 helper 树）的 externalSettle
// 静默期。
const exeImageName = "GoNavi.exe"

// mainWindowTitle GoNavi 主窗口固定标题（阶段 0 实证）。就绪判据用它做精确
// 匹配而非"任意非空标题"：未保存 SQL 确认框等对话框也是顶层可见窗，不能
// 冒充"主窗口已就绪"。
const mainWindowTitle = "GoNavi"

type windowsGoNaviProbe struct{}

// NewGoNaviProbe Windows 实现：进程名 Toolhelp32 枚举 + EnumWindows 主窗判据
// （详见 prober.go 契约说明；窗口枚举全部委托平台公共件 winfocus，见 close_windows.go）。
func NewGoNaviProbe() GoNaviProbe { return &windowsGoNaviProbe{} }

// FindPIDs 经 CreateToolhelp32Snapshot 枚举全部 GoNavi.exe 进程 PID。
// 刻意不查路径（QueryFullProcessImageName 跨用户权限下会失败造成漏报），
// 进程名是唯一稳定标识——litemonitor 同口径。
func (p *windowsGoNaviProbe) FindPIDs() []uint32 {
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

func (p *windowsGoNaviProbe) IsRunning() bool {
	return len(p.FindPIDs()) > 0
}

// WaitForReady 轮询"标题为 GoNavi 的可见主窗"在场（WebView2 初始化完成才有），
// 比进程在场更接近用户可操作。全 PID 参与判定：冷启动与外部实例竞速场
// （便携态无互斥体、无让位语义）主窗一出现即视为可交互，误就绪半径为零。
func (p *windowsGoNaviProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsMainWindowOpen(p.FindPIDs()) {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsMainWindowOpen(p.FindPIDs())
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// IsMainWindowOpen 这些进程中是否存在可见且标题为 "GoNavi" 的顶层窗
// （判据实现见 close_windows.go hasTitleWindowByPIDs，枚举走平台公共件）。
func (p *windowsGoNaviProbe) IsMainWindowOpen(pids []uint32) bool {
	if len(pids) == 0 {
		return false // 无进程在场免枚举（WaitForReady 轮询快路径）
	}
	return hasTitleWindowByPIDs(pids)
}
