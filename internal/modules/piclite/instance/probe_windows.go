//go:build windows

package instance

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mutexName tauri-plugin-single-instance 的 Windows 互斥体命名：
// {identifier}-sim（identifier 来自 tauri.conf.json = "com.piclite.desktop"，
// 插件未启用 semver 特性则跨版本恒定）。插件在应用启动即 CreateMutexW 持有，
// 互斥体存在 = 主实例存活——与 ccswitch/markeron 同款已真机验证语义，
// 本模块另经一次性 OpenMutex 实测确认名称命中。
const mutexName = "com.piclite.desktop-sim"

var (
	modUser32           = syscall.NewLazyDLL("user32.dll")
	procEnumWindows     = modUser32.NewProc("EnumWindows")
	procGetWinThreadPID = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWinVisible    = modUser32.NewProc("IsWindowVisible")
	procGetWinLong      = modUser32.NewProc("GetWindowLongW")
	procGetWinRect      = modUser32.NewProc("GetWindowRect")
)

const (
	gwlExStyle     = ^uintptr(20 - 1) // GWL_EXSTYLE = -20（GetWindowLongW 索引编码）
	wsExToolwindow = 0x00000080       // WS_EX_TOOLWINDOW：工具窗口（含单实例消息窗口）
)

// rect 对应 Win32 RECT（GetWindowRect 出参）。
type rect struct {
	left, top, right, bottom int32
}

type windowsPicProbe struct{}

// NewPicProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewPicProbe() PicLiteProbe { return &windowsPicProbe{} }

func (p *windowsPicProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsPicProbe) WaitForReady(timeout time.Duration) bool {
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

// IsMainWindowOpen 按进程 ID 枚举顶层窗口：存在"可见 + 非工具窗口 + 有一定尺寸"
// 的窗口即视为用户正在使用（空闲退出豁免）。
//
// 为什么不用 FindWindowW(-sic/-siw)：单实例插件的事件窗口恒为 WS_VISIBLE
// （源码注释：必须可见才收得到 WM_PAINT 事件泵），IsWindowVisible 恒真，
// 完全无法反映主窗口显隐；插件消息窗口自身又是 WS_EX_TOOLWINDOW，
// 恰好被本函数的过滤条件排除。标题匹配同样不可取——webview 可通过
// document.title 联动改窗口标题，PID + 形态判定才是稳定契约。
func (p *windowsPicProbe) IsMainWindowOpen(pid uint32) bool {
	if pid == 0 {
		return false
	}
	var open bool
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if open {
			return 0 // 提前终止枚举
		}
		var procID uint32
		procGetWinThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&procID)))
		if procID != pid {
			return 1
		}
		if vis, _, _ := procIsWinVisible.Call(hwnd); vis == 0 {
			return 1
		}
		if ex, _, _ := procGetWinLong.Call(hwnd, gwlExStyle); ex&wsExToolwindow != 0 {
			return 1
		}
		var r rect
		if ret, _, _ := procGetWinRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
			return 1
		}
		if r.right-r.left < 80 || r.bottom-r.top < 80 {
			return 1 // 0x0 隐形消息泵类窗口不算"在用"
		}
		open = true
		return 0
	})
	procEnumWindows.Call(cb, 0)
	return open
}
