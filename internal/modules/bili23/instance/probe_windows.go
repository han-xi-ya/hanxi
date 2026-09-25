//go:build windows

package instance

import (
	"time"

	"golang.org/x/sys/windows"

	win "hanxi/internal/platform/windows"
)

// mutexName 上游 src/main.py 的 APP_MUTEX_NAME：主实例在 Application.__init__
// 里 CreateMutexW 持有，进程存活期间恒在——与 Inno Setup 安装向导检测"程序是否正在运行"
// 用的是同一 GUID（setup.iss AppMutex），经源码实证。窗口无固定类名（Qt 动态注册），
// 故存活判定以互斥体为准，窗口探测走 EnumWindows + PID 归属过滤。
const mutexName = "B096F0C1-D105-4EF9-86E1-5E87DA884EA4"

type windowsBili23Probe struct{}

// NewBili23Probe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewBili23Probe() Bili23Probe { return &windowsBili23Probe{} }

func (p *windowsBili23Probe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsBili23Probe) WaitForReady(timeout time.Duration) bool {
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

// HasVisibleWindow 指定进程是否有带标题的可见顶层窗口。
// Bili23 主窗口、模态询问对话框均计入；收入托盘（主窗口 hide）后返回 false。
// pid 为 0（external 或未启动）直接 false。判据（可见+标题，过滤 Qt 内部
// 消息/工具隐形窗口）委托平台公共件——与本包唤窗同口径；旧写法每次调用
// syscall.NewCallback 现场注册闭包，回调槽池（上限 2000、永不回收）千次探测
// 即爆，公共件静态回调 + lParam 携带状态根治。
func (p *windowsBili23Probe) HasVisibleWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return win.HasFocusableTopWindowForPIDs(map[uint32]struct{}{pid: {}})
}
