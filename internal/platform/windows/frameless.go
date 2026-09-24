//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	// dwmwaWindowCornerPreference DWM 窗口圆角策略属性（Win11 22000+，值 33）。
	dwmwaWindowCornerPreference = 33
	// dwmwcpRound 系统标准圆角裁切（半径由系统统一控制，不可自定义）。
	dwmwcpRound = 2
	// vkLButton 鼠标左键虚拟键码（GetAsyncKeyState 即时态查询用）。
	vkLButton = 0x01
)

var (
	procGetWindowRect    = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowRect")
	procGetAsyncKeyState = syscall.NewLazyDLL("user32.dll").NewProc("GetAsyncKeyState")
)

// windowRect GetWindowRect 输出（物理屏幕坐标；区别于 region.go 的客户端 winRect）。
type windowRect struct {
	Left, Top, Right, Bottom int32
}

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

// MoveWindowBy 按物理像素增量平移窗口（保持尺寸与 Z 序），frameless 跟手拖拽
// 的执行原语。直连 user32 而非 Wails SetPosition：绕开 DIP 换算且不与 Wails
// 内部状态机互相掣肘；下次弹窗仍会按游标重新定位，无需回写。
func MoveWindowBy(hwnd uintptr, dx, dy int32) error {
	var r windowRect
	if rr, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); rr == 0 {
		return fmt.Errorf("GetWindowRect 失败")
	}
	// SetWindowPos 的 X/Y 为 32 位有符号参：负坐标经 uint32 桥接保低位比特。
	r1, _, _ := procSetWindowPos.Call(
		hwnd, 0,
		uintptr(uint32(r.Left+dx)), uintptr(uint32(r.Top+dy)),
		0, 0,
		uintptr(swpNoSize|swpNoZorder),
	)
	if r1 == 0 {
		return fmt.Errorf("SetWindowPos 失败")
	}
	return nil
}

// QueryWindowRect 返回窗口的物理屏幕矩形（left/top/width/height，像素）。
// 缩放会话开手快照用：起点尺寸 + 游标物理位移直接推算目标尺寸，全程物理像素
// 口径（与 MoveWindowBy 同一本账，不碰 Wails DIP 记账）。
func QueryWindowRect(hwnd uintptr) (left, top, width, height int32, err error) {
	var r windowRect
	if rr, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); rr == 0 {
		return 0, 0, 0, 0, fmt.Errorf("GetWindowRect 失败")
	}
	return r.Left, r.Top, r.Right - r.Left, r.Bottom - r.Top, nil
}

// ResizeWindowTo 将窗口设为指定物理像素尺寸（位置与 Z 序不动），frameless
// 跟手缩放（N42②）的执行原语，与 MoveWindowBy 同族同纪律：直连 user32，
// 绕开 Wails DIP 换算；会话收口时由调用方经 SetSize 回写一次保记账一致。
func ResizeWindowTo(hwnd uintptr, width, height int32) error {
	r1, _, _ := procSetWindowPos.Call(
		hwnd, 0, 0, 0,
		uintptr(uint32(width)), uintptr(uint32(height)),
		uintptr(swpNoMove|swpNoZorder),
	)
	if r1 == 0 {
		return fmt.Errorf("SetWindowPos 失败")
	}
	return nil
}

// LeftButtonPressed 鼠标左键当前是否按下（异步即时态）。拖拽轮询用它兜底捕捉
// "指针移出窗口后释放"——前端 mouseup 事件此时可能已丢失。
func LeftButtonPressed() bool {
	r, _, _ := procGetAsyncKeyState.Call(vkLButton)
	return int16(r) < 0 // 高位 0x8000 = 当前按下
}
