//go:build windows

package ocr

import (
	"log/slog"
	"time"

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

// applyCardWindowStyle Acrylic 载体的窗口级精修：DWM 系统圆角裁切，失败只降级
// 外观（直角），不影响功能；Wails 未暴露该属性，直连 dwmapi 补齐。建窗后设置
// 一次即长期生效。
//
// 曾用 DwmExtendFrameIntoClientArea(全负边距) 补无边框投影——实测会把原生标题
// 按钮区"唤醒"到 Acrylic 顶部（右上角凭空多一个 ✕），投影收益不值这个副作用，
// 已撤；Acrylic 毛玻璃本身即具备浮起感。
func applyCardWindowStyle(card *application.WebviewWindow) {
	if err := windows.SetFramelessRoundedCorners(uintptr(card.NativeWindow())); err != nil {
		slog.Debug("ocr: 悬浮卡圆角设置失败", "err", err)
	}
}

// startCardDrag 左键按住跟手：8ms 采样游标物理位移，经 user32 平移窗口（物理
// 坐标直达，绕开 Wails DIP 记账——下次弹出本就按游标重定位，无需回写）。
// 三重退出保障：前端 mouseup → CardDragEnd 关通道；指针移出窗口丢失 mouseup 时
// GetAsyncKeyState 左键态兜底；游标查询失败即停。defer 以通道身份比对清理，
// 连点场景下绝不误杀新会话。goroutine 生命周期与窗口隐藏解耦，退出必回收。
func (s *OcrService) startCardDrag(card *application.WebviewWindow) {
	if card == nil || !windows.LeftButtonPressed() {
		return // 卡片未建或调用间隙左键已松：不产生半路会话
	}
	s.cardDragMu.Lock()
	if s.cardDragStop != nil {
		s.cardDragMu.Unlock()
		return // 已有拖拽会话，防重入
	}
	stop := make(chan struct{})
	s.cardDragStop = stop
	s.cardDragMu.Unlock()

	hwnd := uintptr(card.NativeWindow())
	lastX, lastY, err := s.snip.CursorPos()
	if err != nil {
		s.CardDragEnd()
		return
	}
	go func() {
		defer func() {
			s.cardDragMu.Lock()
			if s.cardDragStop == stop {
				s.cardDragStop = nil
			}
			s.cardDragMu.Unlock()
		}()
		ticker := time.NewTicker(8 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				x, y, err := s.snip.CursorPos()
				if err != nil || !windows.LeftButtonPressed() {
					return
				}
				if dx, dy := x-lastX, y-lastY; dx != 0 || dy != 0 {
					if err := windows.MoveWindowBy(hwnd, int32(dx), int32(dy)); err != nil {
						slog.Debug("ocr: 悬浮卡拖拽平移失败", "err", err)
						return
					}
					lastX, lastY = x, y
				}
			}
		}
	}()
}
