//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modUser32Region = syscall.NewLazyDLL("user32.dll")
	modGdi32Region  = syscall.NewLazyDLL("gdi32.dll")

	procGetClientRect     = modUser32Region.NewProc("GetClientRect")
	procSetWindowRgn      = modUser32Region.NewProc("SetWindowRgn")
	procCreateEllipticRgn = modGdi32Region.NewProc("CreateEllipticRgn")
	procDeleteObject      = modGdi32Region.NewProc("DeleteObject")
)

// winRect 对应 RECT（客户端坐标，物理像素）。
type winRect struct{ left, top, right, bottom int32 }

// ClipWindowEllipse 将窗口客户端区裁剪为椭圆区域：方形窗口即得正圆。
//
// 椭圆之外系统层面既不绘制也不接收鼠标输入（四角点击穿透到下层应用）。配合
// 真透明窗口使用时，把裁剪圈设在视觉内容（含投影）淡出之后的半径上，GDI 区域
// 边缘无抗锯齿的硬边就落在全透明区、视觉不可见，本函数只承担命中测试职责。
// 区域按物理像素固定、不随窗口改尺寸自动更新，重设安全幂等。注意"DIP 尺寸恒定"
// 的窗口跨到不同缩放比的显示器后物理像素同样会变，此时必须重裁，不可一次了结。
func ClipWindowEllipse(hwnd uintptr) error {
	var r winRect
	if ret, _, err := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return fmt.Errorf("GetClientRect 失败: %w", err)
	}
	if r.right <= 0 || r.bottom <= 0 {
		return fmt.Errorf("客户端区尺寸无效（%d×%d），窗口可能尚未创建", r.right, r.bottom)
	}
	hrgn, _, err := procCreateEllipticRgn.Call(0, 0, uintptr(r.right), uintptr(r.bottom))
	if hrgn == 0 {
		return fmt.Errorf("CreateEllipticRgn 失败: %w", err)
	}
	// SetWindowRgn 成功后区域句柄所有权移交窗口，绝不可再 DeleteObject；
	// 仅在失败路径自行回收，避免反复重试泄露 GDI 对象。
	if ret, _, err := procSetWindowRgn.Call(hwnd, hrgn, 1); ret == 0 {
		procDeleteObject.Call(hrgn)
		return fmt.Errorf("SetWindowRgn 失败: %w", err)
	}
	return nil
}
