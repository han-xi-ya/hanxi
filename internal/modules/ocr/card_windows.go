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

// startCardResize 右下角把手左键按住跟手缩放（N42②）：与 startCardDrag 同族
// ——8ms 采样游标物理位移，起点窗口尺寸 + 位移直接算目标（全程物理像素），
// ResizeWindowTo 落窗；钳制下界保正文可读、上界防巨卡（工作区回收由下次
// 弹出的 positionSnipCard 兜底）。退出三重保障同拖拽：CardResizeEnd 关通道、
// 左键态兜底、游标查询失败即停。**收口必落账**：以 DPI 缩放系数换算 DIP，
// SetSize 回写 Wails 记账 + store 持久化（下次弹出复现记忆尺寸）；换算不可得
// （Screen 缺失）则只回写保守默认比例 1.0 并告警日志，下次弹出仍可正常显示。
func (s *OcrService) startCardResize(card *application.WebviewWindow) {
	if card == nil || !windows.LeftButtonPressed() {
		return // 卡片未建或调用间隙左键已松：不产生半路会话
	}
	s.cardResizeMu.Lock()
	if s.cardResizeStop != nil {
		s.cardResizeMu.Unlock()
		return // 已有缩放会话，防重入
	}
	stop := make(chan struct{})
	s.cardResizeStop = stop
	s.cardResizeMu.Unlock()

	hwnd := uintptr(card.NativeWindow())
	_, _, startW, startH, err := windows.QueryWindowRect(hwnd)
	x0, y0, cerr := s.snip.CursorPos()
	if err != nil || cerr != nil {
		s.CardResizeEnd()
		return
	}
	go func() {
		defer func() {
			s.cardResizeMu.Lock()
			if s.cardResizeStop == stop {
				s.cardResizeStop = nil
			}
			s.cardResizeMu.Unlock()
			s.persistCardSize(card)
		}()
		ticker := time.NewTicker(8 * time.Millisecond)
		defer ticker.Stop()
		curW, curH := startW, startH
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				x, y, err := s.snip.CursorPos()
				if err != nil || !windows.LeftButtonPressed() {
					return
				}
				// 目标尺寸钳进物理允许域（下界按 1x 保守取 DIP 最小档，高 DPI 下
				// 只会更宽松；DIP 精确钳制在 persistCardSize 收口时兜底执行）。
				// CursorPos 返回 int，位移转 int32 与窗口矩形同口径。
				nw, nh := startW+int32(x-x0), startH+int32(y-y0)
				if nw < cardDIPMinW {
					nw = cardDIPMinW
				}
				if nh < cardDIPMinH {
					nh = cardDIPMinH
				}
				if nw > cardDIPMaxW*2 { // 物理上界=2x DIP 顶格，防失控巨窗
					nw = cardDIPMaxW * 2
				}
				if nh > cardDIPMaxH*2 {
					nh = cardDIPMaxH * 2
				}
				if nw == curW && nh == curH {
					continue
				}
				if err := windows.ResizeWindowTo(hwnd, nw, nh); err != nil {
					slog.Debug("ocr: 悬浮卡缩放失败", "err", err)
					return
				}
				curW, curH = nw, nh
			}
		}
	}()
}

// persistCardSize 缩放会话收口落账：物理尺寸经光标屏缩放系数换算 DIP，
// SetSize 回写 Wails 记账（保持后续 Wails 路径与原生现实一致）并持久化。
func (s *OcrService) persistCardSize(card *application.WebviewWindow) {
	l, t, w, h, err := windows.QueryWindowRect(uintptr(card.NativeWindow()))
	if err != nil {
		dw, dh := card.Size() // Wails DIP 读数兜底（不做换算）
		_ = s.store.SetSnipCardSize(dw, dh)
		return
	}
	scale := 1.0
	if a := application.Get(); a != nil && a.Screen != nil {
		if scr := a.Screen.ScreenNearestPhysicalPoint(application.Point{X: int(l), Y: int(t)}); scr != nil && scr.ScaleFactor > 0 {
			scale = float64(scr.ScaleFactor)
		}
	}
	dw, dh := int(float64(w)/scale+0.5), int(float64(h)/scale+0.5)
	card.SetSize(dw, dh)
	if err := s.store.SetSnipCardSize(dw, dh); err != nil {
		slog.Warn("ocr: 悬浮卡尺寸记忆落盘失败", "err", err)
	}
}
