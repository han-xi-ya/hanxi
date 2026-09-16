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
