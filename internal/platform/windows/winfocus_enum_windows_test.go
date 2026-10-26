//go:build windows

package windows

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── 唤窗公共件的派发回归测试（A2 2026-10-26）──────────────────────────────
//
// 病灶档案：EnumTopWindows 曾以 uint64 存 ctx 键、以回调入参 uintptr 取键，
// sync.Map 键相等要求类型一致 → Load 恒 miss → wfEnumProc 首窗即 return 0
// 中止枚举。真机探针实证：交互桌面下 visited=0，全部唤窗/在窗探测
// （FocusTopWindowForPIDs / HasFocusableTopWindowForPIDs / 各模块
// close_windows 直接枚举）静默失效。
//
// 测试策略：造一个**真实的自有窗口**（屏外、可见、带标题）当外部锚点，
// 断言枚举必须报出它的 hwnd 与属主 pid——派发再断必红。旧压测
// "seen==0 即 skip"恰好掩蔽过这条断链，本文件的 Skip 只允许由
// "无法创建窗口"（无交互桌面会话）触发，枚举到 0 一律 Fail。

var (
	procProbeRegisterClassExW = syscall.NewLazyDLL("user32.dll").NewProc("RegisterClassExW")
	procProbeCreateWindowExW  = syscall.NewLazyDLL("user32.dll").NewProc("CreateWindowExW")
	procProbeDestroyWindow    = syscall.NewLazyDLL("user32.dll").NewProc("DestroyWindow")
	procProbeUnregisterClassW = syscall.NewLazyDLL("user32.dll").NewProc("UnregisterClassW")
	procProbeDefWindowProcW   = syscall.NewLazyDLL("user32.dll").NewProc("DefWindowProcW")
)

// wndClassExW 对应 WNDCLASSEXW（x/sys 未封装该结构，测试本地声明）。
type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

// probeWndProc 静态窗口过程：直通 DefWindowProcW（窗口只作枚举锚点，不处理业务消息）。
var probeWndProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
	ret, _, _ := procProbeDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
})

// spawnProbeWindow 在当前（已 LockOSThread 的）线程创建一个屏外可见、带标题的
// 顶层窗口，返回 hwnd；失败（典型为无交互桌面会话）返回 error。
func spawnProbeWindow(title string) (uintptr, error) {
	cls, err := syscall.UTF16PtrFromString("hanxiA2WinfocusProbeClass")
	if err != nil {
		return 0, err
	}
	ttl, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		Style:         0x0003, // CS_HREDRAW|CS_VREDRAW
		LpfnWndProc:   probeWndProc,
		LpszClassName: cls,
	}
	// hInstance 置 0：RegisterClassExW/CreateWindowExW 允许 NULL 表示主模块。
	r, _, e := procProbeRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 && errnoCode(e) != 1411 { // 1411 = ERROR_CLASS_ALREADY_EXISTS（同进程重复注册容忍）
		return 0, e
	}
	const wsPopup, wsVisible = 0x80000000, 0x10000000
	off := int32(-32000) // 屏外摆位，不干扰机主（负坐标经变量转换保低位比特）
	h, _, e := procProbeCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(ttl)),
		uintptr(wsPopup|wsVisible),
		uintptr(off), uintptr(off), 8, 8,
		0, 0, 0, 0,
	)
	if h == 0 {
		procProbeUnregisterClassW.Call(uintptr(unsafe.Pointer(cls)), 0)
		return 0, e
	}
	return h, nil
}

// errnoCode 从 LazyProc.Call 的 error 返回值中提取 Win32 错误码（非 Errno 形态返 0）。
func errnoCode(err error) syscall.Errno {
	if en, ok := err.(syscall.Errno); ok {
		return en
	}
	return 0
}

func destroyProbeWindow(hwnd uintptr) {
	procProbeDestroyWindow.Call(hwnd)
	if cls, err := syscall.UTF16PtrFromString("hanxiA2WinfocusProbeClass"); err == nil {
		procProbeUnregisterClassW.Call(uintptr(unsafe.Pointer(cls)), 0)
	}
}

// TestEnumTopWindowsDispatchesRealWindow 断言外部事实：枚举回调必须被
// 系统派发到本测试自建的顶层窗口（hwnd 与属主 pid 双对账）。
func TestEnumTopWindowsDispatchesRealWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd, err := spawnProbeWindow("hanxi A2 winfocus probe")
	if err != nil {
		t.Skipf("当前会话无法创建顶层窗口（典型为无交互桌面会话）: %v", err)
	}
	defer destroyProbeWindow(hwnd)

	ownPid := uint32(os.Getpid())
	var (
		mu        sync.Mutex
		total     int
		sawHwnd   bool
		sawOwnPid bool
	)
	EnumTopWindows(func(h uintptr, pid uint32) bool {
		mu.Lock()
		defer mu.Unlock()
		total++
		if h == hwnd {
			sawHwnd = true
		}
		if pid == ownPid {
			sawOwnPid = true
		}
		return true
	})
	if total == 0 {
		t.Fatal("枚举回调一次都未派发：窗口已创建成功、桌面在场——派发断链复发")
	}
	if !sawHwnd || !sawOwnPid {
		t.Fatalf("枚举必须包含自建窗口（sawHwnd=%v sawOwnPid=%v，共 %d 窗）", sawHwnd, sawOwnPid, total)
	}
}

// TestHasFocusableTopWindowSeesProbeWindow 在窗探测同锚点：可见+带标题的
// 自建窗口必须被 HasFocusableTopWindowForPIDs 判为在场（修复前恒 false）；
// 对照组假 PID 继续报 false（探测不是恒真门）。
func TestHasFocusableTopWindowSeesProbeWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd, err := spawnProbeWindow("hanxi A2 presence probe")
	if err != nil {
		t.Skipf("当前会话无法创建顶层窗口（无交互桌面会话）: %v", err)
	}
	defer destroyProbeWindow(hwnd)

	if !HasFocusableTopWindowForPIDs(map[uint32]struct{}{uint32(os.Getpid()): {}}) {
		t.Fatal("自建可见带标题窗在场，在窗探测却报无窗——派发断链复发")
	}
	if HasFocusableTopWindowForPIDs(map[uint32]struct{}{0x7FFFFFFF: {}}) {
		t.Fatal("不存在的 PID 不应有可聚焦窗")
	}
}

// TestEnumTopWindowsConcurrentContexts 并发枚举各自 ctx 必须互不串台
// （ID 查表设计的核心承诺；计数写各自槽位无需再加锁）。
func TestEnumTopWindowsConcurrentContexts(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd, err := spawnProbeWindow("hanxi A2 concurrent probe")
	if err != nil {
		t.Skipf("当前会话无法创建顶层窗口（无交互桌面会话）: %v", err)
	}
	defer destroyProbeWindow(hwnd)

	var wg sync.WaitGroup
	counts := make([]int, 4)
	for idx := range counts {
		wg.Go(func() {
			EnumTopWindows(func(uintptr, uint32) bool {
				counts[idx]++
				return true
			})
		})
	}
	wg.Wait()
	for i, c := range counts {
		if c == 0 {
			t.Fatalf("并发枚举 #%d 一次回调都没收到：ctx 查表并发漂移复发", i)
		}
	}
}
