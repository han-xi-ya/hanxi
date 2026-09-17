//go:build windows

package ocr

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/platform/windows"
)

// forceCardForeground Wails 裸 Focus 在后台进程会被前台锁拒绝（主窗藏托盘时），
// 借 quickmenu 同款输入特权强制置前；失败仅影响 Esc 直达，不致命。
func forceCardForeground(card *application.WebviewWindow) {
	if err := windows.SetForegroundForce(uintptr(card.NativeWindow())); err != nil {
		slog.Debug("ocr: 悬浮卡强制置前失败", "err", err)
	}
}

// applyCardWindowStyle Acrylic 载体的窗口级精修：DWM 系统圆角裁切 + 无边框
// 投影。两者失败都只降级外观（直角/无阴影），不影响功能；Wails 未暴露这两个
// 属性，直连 dwmapi 补齐。建窗后设置一次即长期生效。
func applyCardWindowStyle(card *application.WebviewWindow) {
	hwnd := uintptr(card.NativeWindow())
	if err := windows.SetFramelessRoundedCorners(hwnd); err != nil {
		slog.Debug("ocr: 悬浮卡圆角设置失败", "err", err)
	}
	if err := windows.EnableFramelessShadow(hwnd); err != nil {
		slog.Debug("ocr: 悬浮卡投影设置失败", "err", err)
	}
}
