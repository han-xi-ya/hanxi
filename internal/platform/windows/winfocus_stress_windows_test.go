//go:build windows

package windows

import "testing"

// 回调槽防回归压测：静态回调 + lParam 模式下，任意多次枚举不得新占
// syscall.NewCallback 槽（池上限 2000、闭包注册永不回收——超限即
// fatal error 不可 recover，常驻应用等于定时崩溃）。2500 轮含提前
// 停止路径，全过即证明零消耗。
func TestEnumTopWindowsNoCallbackLeak(t *testing.T) {
	for i := 0; i < 2500; i++ {
		seen := 0
		EnumTopWindows(func(hwnd uintptr, pid uint32) bool {
			seen++
			return true
		})
		if i%500 == 0 && seen == 0 {
			t.Skip("无桌面会话/枚举不可用，压测不适用")
		}
	}
	// 中途停止路径同样零消耗
	for i := 0; i < 500; i++ {
		EnumTopWindows(func(hwnd uintptr, pid uint32) bool { return false })
	}
}
