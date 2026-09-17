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

// 外壳层调色表：标题栏底色对齐 --surface-chrome（DWM 标题栏与双栏导航共色，
// Fluent 式"壳"与内容区分层），文字对齐 --color-text。与
// frontend/src/styles/tokens.css 各色板块逐值对齐；tokens 改动时必须同步本表。
// 键为 data-accent 色板名，值为 [2]{浅色模式, 深色模式}。
type chromePair struct{ caption, text uint32 }

var chromePalettes = map[string][2]chromePair{
	"teal": {{rgb(0xe9, 0xf5, 0xf5), rgb(0x14, 0x25, 0x2c)}, {rgb(0x0b, 0x18, 0x1d), rgb(0xe8, 0xf2, 0xf3)}},
	"sky":  {{rgb(0xe8, 0xf1, 0xf8), rgb(0x19, 0x20, 0x29)}, {rgb(0x1a, 0x20, 0x26), rgb(0xed, 0xf0, 0xf6)}},
	"iris": {{rgb(0xec, 0xe7, 0xf9), rgb(0x21, 0x1d, 0x29)}, {rgb(0x20, 0x1c, 0x27), rgb(0xf1, 0xef, 0xf5)}},
	"jade": {{rgb(0xe8, 0xf1, 0xf2), rgb(0x14, 0x22, 0x22)}, {rgb(0x14, 0x21, 0x21), rgb(0xea, 0xf2, 0xf2)}},
	"onyx": {{rgb(0xdc, 0xe6, 0xf5), rgb(0x0a, 0x0f, 0x14)}, {rgb(0x09, 0x0c, 0x11), rgb(0xf2, 0xf6, 0xfb)}},
}

// SetTitleBarPalette 将原生标题栏底色/文字精确对齐当前色板的外壳层（替代沉浸式
// 深色的系统默认灰调）。仅 Win11 生效；Win10 返回错误，调用方静默降级即可。
// 未知色板回退青壳（与前端 tokens.css 无 data-accent 时的回退行为一致）。
func SetTitleBarPalette(hwnd uintptr, dark bool, accent string) error {
	pair, ok := chromePalettes[accent]
	if !ok {
		pair = chromePalettes["teal"]
	}
	caption, text := pair[0].caption, pair[0].text
	if dark {
		caption, text = pair[1].caption, pair[1].text
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
