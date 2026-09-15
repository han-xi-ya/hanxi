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
// Wails beta.10 Windows 侧没有分层/透明窗口能力，圆盘弹窗改用 GDI 区域模拟
// 圆形轮廓——椭圆之外系统层面既不绘制也不接收鼠标输入，四角不是"透明像素"
// 而是整块不存在，是 Windows 上做圆形浮窗的标准手法（无需 DWM、零逐帧开销）。
// 已知代价：GDI 区域边缘无抗锯齿，低 DPI 下有轻微锯齿，视觉上把环形描边画在
// 裁剪半径稍内侧来弱化。窗口尺寸变化后需重设（区域按旧物理尺寸会失真），
// 常驻固定尺寸的弹窗不受影响。
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
