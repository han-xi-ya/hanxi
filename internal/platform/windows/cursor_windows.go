//go:build windows

package windows

import "unsafe"

var procCursorPos = modUser32Fg.NewProc("GetCursorPos")

// CursorPos 返回鼠标光标当前的物理像素坐标。
// 供"跟随手头显示器"类唤窗摆位使用（如速记卡落点判定所属显示器）；
// 失败（罕见：无桌面会话等）上抛错误由调用方决定回落。
func CursorPos() (x, y int, err error) {
	var p struct{ X, Y int32 }
	if r, _, e := procCursorPos.Call(uintptr(unsafe.Pointer(&p))); r == 0 {
		return 0, 0, e
	}
	return int(p.X), int(p.Y), nil
}
