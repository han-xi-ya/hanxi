//go:build windows

package windows

import (
	"fmt"
	"unsafe"
)

const (
	// dwmwaWindowCornerPreference DWM 窗口圆角策略属性（Win11 22000+，值 33）。
	dwmwaWindowCornerPreference = 33
	// dwmwcpRound 系统标准圆角裁切（半径由系统统一控制，不可自定义）。
	dwmwcpRound = 2
)

// procDwmExtendFrameIntoClientArea 与 procDwmSetWindowAttribute 同属 dwmapi，
// 句柄复用 darkmode.go 的 modDwmapi/procDwmSetWindowAttribute。
var procDwmExtendFrameIntoClientArea = modDwmapi.NewProc("DwmExtendFrameIntoClientArea")

// margins DWM 玻璃边距（物理像素；全负值 = 整个客户区）。
type margins struct{ Left, Right, Top, Bottom int32 }

// SetFramelessRoundedCorners 让 DWM 按 Win11 系统标准半径裁切窗口（frameless
// 窗口同样生效）。Acrylic/透明壳卡片的圆角必须由系统裁切——页内 border-radius
// 只能裁页面底色，裁不掉 DWM 铺满窗口矩形的 backdrop。
func SetFramelessRoundedCorners(hwnd uintptr) error {
	value := int32(dwmwcpRound)
	r1, _, _ := procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaWindowCornerPreference),
		uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Sizeof(value)),
	)
	if hr := uint32(r1); hr != 0 {
		return fmt.Errorf("DwmSetWindowAttribute(圆角) 调用失败 (HRESULT: 0x%08x)", hr)
	}
	return nil
}

// EnableFramelessShadow 无边框窗口默认没有系统投影；把玻璃边距设为整窗（全 -1）
// 让 DWM 按标准浮窗绘制 drop shadow（sheet 手法，Win7 时代沿用至今）。失败不致命，
// 仅损失投影层次。
func EnableFramelessShadow(hwnd uintptr) error {
	m := margins{Left: -1, Right: -1, Top: -1, Bottom: -1}
	r1, _, _ := procDwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&m)))
	if hr := uint32(r1); hr != 0 {
		return fmt.Errorf("DwmExtendFrameIntoClientArea 调用失败 (HRESULT: 0x%08x)", hr)
	}
	return nil
}
