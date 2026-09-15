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
	// dwmwaCaptionColor / dwmwaTextColor DWM 窗口属性：标题栏底色与文字精确配色
	// （Win11 22000+ 支持；值 0xFFFFFFFF 表示恢复系统默认）。
	dwmwaCaptionColor = 35
	dwmwaTextColor    = 36
	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZorder       = 0x0004
	swpFrameChanged   = 0x0020
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

// rgb 打包 COLORREF（0x00BBGGRR）。
func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

// 工作台两套主题的页面底色/主文字色（与 frontend/src/styles/tokens.css 的
// --surface-page 与 --color-text 逐值对齐；tokens 改动时必须同步本表）。
var (
	chromeLightCaption = rgb(0xf3, 0xf7, 0xf8)
	chromeLightText    = rgb(0x14, 0x25, 0x2c)
	chromeDarkCaption  = rgb(0x07, 0x13, 0x18)
	chromeDarkText     = rgb(0xe8, 0xf2, 0xf3)
)

// SetTitleBarPalette 将原生标题栏底色/文字精确对齐工作台页面背景（替代沉浸式
// 深色的系统默认灰调）。仅 Win11 生效；Win10 返回错误，调用方静默降级即可。
func SetTitleBarPalette(hwnd uintptr, dark bool) error {
	caption, text := chromeLightCaption, chromeLightText
	if dark {
		caption, text = chromeDarkCaption, chromeDarkText
	}
	hrCaption, _, _ := procDwmSetWindowAttribute.Call(
		hwnd, uintptr(dwmwaCaptionColor),
		uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	if hr := uint32(hrCaption); hr != 0 {
		return fmt.Errorf("DwmSetWindowAttribute 标题栏配色失败 (HRESULT: 0x%08x)", hr)
	}
	// 文字色尽力而为：失败不影响底色已生效。
	_, _, _ = procDwmSetWindowAttribute.Call(
		hwnd, uintptr(dwmwaTextColor),
		uintptr(unsafe.Pointer(&text)), unsafe.Sizeof(text))
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(swpNoSize|swpNoMove|swpNoZorder|swpFrameChanged))
	return nil
}
