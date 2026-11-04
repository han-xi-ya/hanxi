//go:build windows

package instance

import (
	"errors"
	"strings"
	"syscall"
	"time"
	"unsafe"

	win "hanxi/internal/platform/windows"
)

var (
	modUser32Cmd  = syscall.NewLazyDLL("user32.dll")
	procPostMsgW  = modUser32Cmd.NewProc("PostMessageW")
	procClassName = modUser32Cmd.NewProc("GetClassNameW")
)

const (
	// wmCommand/wmClose Win32 标准消息号（上游 wndproc 的两条 match 分支）。
	wmCommand = 0x0111
	wmClose   = 0x0010

	// messageWindowClass 上游常驻隐藏消息窗的窗口类名（src/app/mod.rs 的
	// WINDOW_CLASS，CreateWindowExW 时标题为 "AirSnip"，dwStyle=0 故不可见）。
	// 它是本模块所有外部动作的唯一投递目标：托盘菜单、热键、贴图回传都汇到它。
	messageWindowClass = "airsnip.hidden"

	// errorElevationRequired CreateProcessW 对 requireAdministrator 清单目标的
	// 直接失败码——未提权的 Hanxi 拉起 AirSnip 时得到它（不会代弹 UAC）。
	errorElevationRequired syscall.Errno = 740
)

// AirSnip 托盘菜单命令 ID（上游 src/app/mod.rs 的 IDM_* 常量，与 src/app/tray.rs
// 菜单项文字一一对应）。这三个是"截图 / 从剪贴板贴图 / 设置"的唯一可编程入口：
// 上游没有 CLI、没有命名管道、没有唤窗回调，第二实例投递的 WM_APP+2 甚至无人处理。
// 第四个 IDM_EXIT=40004 不在这里定义——它的实现本身就是"给自己投 WM_CLOSE"，
// 由 postClose 直接承担，绕一层菜单 ID 反而多一处耦合。
const (
	CmdShot     = 40001
	CmdPin      = 40002
	CmdSettings = 40003
)

// windowReadyWait 投递前等待消息窗在场的上限。冷启动 spawn 成功 ≠ 窗口已注册
// （上游 run() 里建窗早于托盘/热键，但进程枚举与窗口之间仍有毫秒级空窗），
// 投给一个还不存在的窗口等于什么都没做。包级变量：单测压缩、生产保持 3s。
var windowReadyWait = 3 * time.Second

// 上游四类顶层窗的窗口类名（源码实证：src/app/mod.rs、src/overlay/setup.rs、
// src/pin/window.rs、src/settings/panel/mod.rs 的 WINDOW_CLASS 常量）。
// hidden 是常驻消息窗（命令投递目标），其余三个都是"动作真的生效了"的外部判据：
// 投 40001 后 airsnip.overlay 必须出现、投 40003 后 airsnip.settings 必须出现、
// 投 40002 且剪贴板有位图时 airsnip.pin 必须出现。
const (
	WindowClassHidden   = messageWindowClass
	WindowClassOverlay  = "airsnip.overlay"
	WindowClassPin      = "airsnip.pin"
	WindowClassSettings = "airsnip.settings"
)

// WindowPresentByClass 目标实例是否存在指定类名的顶层窗（pid==0 不限属主）。
// 这是命令通道的**外部判据**：PostMessage 返 true 只证明消息入队，UIPI 丢弃、
// 上游改版吞掉命令都照样"成功"——验收必须看窗是否真出现，不看返回值自证。
func WindowPresentByClass(class string, pid uint32) bool {
	return findWindowByClass(class, pid) != 0
}

// findWindowByClass 按类名（可选 PID 收窄）定位顶层窗，未命中返回 0。
func findWindowByClass(class string, pid uint32) uintptr {
	var hit uintptr
	win.EnumTopWindows(func(hwnd uintptr, wpid uint32) bool {
		if pid != 0 && wpid != pid {
			return true
		}
		if strings.EqualFold(windowClassName(hwnd), class) {
			hit = hwnd
			return false
		}
		return true
	})
	return hit
}

// waitForWindowByClass 在 timeout 内轮询指定类名窗在场，返回 hwnd（超时 0）。
func waitForWindowByClass(class string, pid uint32, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for {
		if hwnd := findWindowByClass(class, pid); hwnd != 0 {
			return hwnd
		}
		if !time.Now().Before(deadline) {
			return 0
		}
		time.Sleep(80 * time.Millisecond)
	}
}

// windowClassName 取窗口类名（失败返回空串，调用方按"不匹配"处理）。
func windowClassName(hwnd uintptr) string {
	var buf [128]uint16
	n, _, _ := procClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// findMessageWindow 定位 AirSnip 的隐藏消息窗。pid==0 表示不限属主（外部实例
// 场景）；未找到返回 0。枚举委托平台公共件（静态回调，绝不在此自建
// syscall.NewCallback——回调槽上限 2000 且永不回收，见 winfocus_windows.go 根治模板）。
func findMessageWindow(pid uint32) uintptr {
	var hit uintptr
	win.EnumTopWindows(func(hwnd uintptr, wpid uint32) bool {
		if pid != 0 && wpid != pid {
			return true // 继续枚举
		}
		if strings.EqualFold(windowClassName(hwnd), messageWindowClass) {
			hit = hwnd
			return false // 命中即停
		}
		return true
	})
	return hit
}

// waitForMessageWindow 在 timeout 内轮询消息窗在场，返回 hwnd（超时返回 0）。
func waitForMessageWindow(pid uint32, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for {
		if hwnd := findMessageWindow(pid); hwnd != 0 {
			return hwnd
		}
		if !time.Now().Before(deadline) {
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// postCommand 向消息窗投递上游菜单命令（与用户手点托盘菜单完全同构）。
// 返回 false = 目标窗口不在场（未运行/已退出）。UIPI 注意：Hanxi 与实例完整性
// 不同时这里仍会"投递成功"而目标静默丢弃，故调用方一律以状态复探为准、
// 不承诺必达（snipaste 信使同纪律）。
func postCommand(pid uint32, id uint32) bool {
	hwnd := waitForMessageWindow(pid, windowReadyWait)
	if hwnd == 0 {
		return false
	}
	r, _, _ := procPostMsgW.Call(hwnd, uintptr(wmCommand), uintptr(id), 0)
	return r != 0
}

// postClose 向自有实例的消息窗投递 WM_CLOSE：上游 wndproc 的 WM_DESTROY 分支
// PostQuitMessage(0) → 主消息循环退出 → 进程收口，与用户点托盘「退出」逐字等价。
// 只按 pid 匹配（pid==0 直接放弃），绝不给外部实例投退出消息。
func postClose(pid uint32) bool {
	if pid == 0 {
		return false
	}
	hwnd := findMessageWindow(pid)
	if hwnd == 0 {
		return false
	}
	r, _, _ := procPostMsgW.Call(hwnd, uintptr(wmClose), 0, 0)
	return r != 0
}

// focusWindow 唤起该 PID 的可聚焦顶层窗（设置面板 "airsnip.settings"），
// 委托平台公共件（可见+有标题过滤、IsIconic 恢复、借权置前三要素收口）。
// 投递 CmdSettings 后面板才存在，调用方需先给窗口一点创建时间再唤。
func focusWindow(pid uint32) bool {
	if pid == 0 {
		return false
	}
	return win.FocusTopWindowForPID(pid)
}

// elevateHint 识别提权需求错误：AirSnip manifest 为 requireAdministrator（ADR 0003
// 要求它能覆盖/操作管理员窗口），未提权父进程 CreateProcess 直接失败（740）
// 且系统不代弹 UAC。返回面向用户的指引文案；非该错误返回空串。
// 文案必须含「管理员」关键词——前端 ElevateRestart 一键提权重启钮据此显隐。
func elevateHint(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errorElevationRequired {
		return "AirSnip 要求管理员权限运行（上游 manifest 强制，否则无法覆盖管理员窗口）：请以管理员身份重新启动 Hanxi 后重试"
	}
	return ""
}

// ElevateHint 导出别名：service 层的 RPC 错误通道与引擎广播快照通道共用同一
// 判据（heukms 双通道同型），避免两条路各写一套 740 匹配漂移出词表分叉。
func ElevateHint(err error) string { return elevateHint(err) }
