//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	// dwmwaUseImmersiveDarkMode DWM 窗口属性：标题栏沉浸式深色（Win10 1809+ 正式值 20）。
	dwmwaUseImmersiveDarkMode = 20
	swpNoSize                 = 0x0001
	swpNoMove                 = 0x0002
	swpNoZorder               = 0x0004
	swpFrameChanged           = 0x0020
)

var (
	modDwmapi                 = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
	procSetWindowPos          = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")
)

// SetImmersiveDarkMode 切换指定 HWND 的 Win32 原生标题栏亮/暗。
// 这是前端深色主题同步到原生窗框的唯一桥梁（webview 内容管不到 DWM 非客户区），
// 由 AppService.SetWindowDarkMode 桥接给前端 useTheme 调用。
func SetImmersiveDarkMode(hwnd uintptr, dark bool) error {
	value := int32(0)
	if dark {
		value = 1
	}
	r1, _, _ := procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Sizeof(value)),
	)
	if hr := uint32(r1); hr != 0 {
		return fmt.Errorf("DwmSetWindowAttribute 调用失败 (HRESULT: 0x%08x)", hr)
	}
	// 触发一次非客户区重算，让标题栏配色立即生效而非等下一次窗口事件。
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(swpNoSize|swpNoMove|swpNoZorder|swpFrameChanged))
	return nil
}
