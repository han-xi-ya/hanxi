// 悬浮结果卡：框选截屏识别的轻量出口（轮盘场景不弹主窗）。
// 窗口形态：frameless + 置顶 + 不进任务栏，常驻隐藏复用（beta.10 无公开窗口
// 销毁 API，隐藏复用与托盘驻留策略同构）。方案 D：背景用 Win11 原生 Acrylic
// （真桌面毛玻璃）——窗口即卡片，圆角与投影由 DWM 裁切/绘制，页面不再自绘壳。
// 与轮盘弹窗的唯一差异：**不挂失焦自动隐藏**——用户要能在卡内手动选字复制。
package ocr

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	cardWindowName  = "ocr-snip-card"
	cardWidthDIP    = 420
	cardHeightDIP   = 240
	cardCursorGap   = 16 // 卡片左上角相对游标的右下偏移（DIP）
	cardEventResult = "ocr:snip-result"
)

// showSnipCard 记录结果并按需建卡、定位、置前、广播。
// 定位失败/拿不到游标只降级为"用窗口上次位置"，不打断识别闭环。
func (s *OcrService) showSnipCard(res SnipResult) {
	s.cardMu.Lock()
	s.lastSnip = res
	card := s.card
	s.cardMu.Unlock()

	a := application.Get()
	if a == nil || a.Window == nil {
		slog.Warn("ocr: 应用实例不可用，悬浮卡未弹出")
		return
	}
	if card == nil {
		card = a.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             cardWindowName,
			Title:            "识别结果",
			Width:            cardWidthDIP,
			Height:           cardHeightDIP,
			Hidden:           true,
			Frameless:        true,
			AlwaysOnTop:      true,
			DisableResize:    true,
			BackgroundType:   application.BackgroundTypeTranslucent,
			Windows:          application.WindowsWindow{HiddenOnTaskbar: true, BackdropType: application.Acrylic},
			URL:              "/#ocrcard",
			BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		})
		applyCardWindowStyle(card)
		card.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
			ev.Cancel()
			card.Hide()
		})
		s.cardMu.Lock()
		s.card = card
		s.cardMu.Unlock()
	}

	s.positionSnipCard(card)
	card.Show()
	forceCardForeground(card)
	if a.Event != nil {
		a.Event.Emit(cardEventResult, s.cardResult())
	}
}

// positionSnipCard 游标物理坐标 → DIP + 就近屏工作区钳位（与 quickmenu
// showAt 同一套换算，卡片取游标右下贴放而非居中）。
func (s *OcrService) positionSnipCard(card *application.WebviewWindow) {
	a := application.Get()
	if a == nil || a.Screen == nil {
		return
	}
	x, y, err := s.snip.CursorPos()
	if err != nil {
		slog.Debug("ocr: 游标位置不可得，悬浮卡沿用上一次位置", "err", err)
		return
	}
	physical := application.Point{X: x, Y: y}
	dip := a.Screen.PhysicalToDipPoint(physical)
	w, h := card.Size()
	px, py := dip.X+cardCursorGap, dip.Y+cardCursorGap
	if scr := a.Screen.ScreenNearestPhysicalPoint(physical); scr != nil {
		wa := scr.WorkArea
		if px+w > wa.X+wa.Width {
			px = wa.X + wa.Width - w
		}
		if py+h > wa.Y+wa.Height {
			py = wa.Y + wa.Height - h
		}
		if px < wa.X {
			px = wa.X
		}
		if py < wa.Y {
			py = wa.Y
		}
	}
	card.SetPosition(px, py)
}

// ---------- 卡片视图绑定方法（/#ocrcard） ----------

// cardResult 最近一次截屏识别结果快照。
func (s *OcrService) cardResult() SnipResult {
	s.cardMu.Lock()
	defer s.cardMu.Unlock()
	return s.lastSnip
}

// GetSnipResult 卡片挂载/刷新时拉取当前结果（事件双保险，常驻窗体不漏帧）。
// found 仅在存在成功结果时为真——取消帧不进卡片通道（showSnipCard 只在成功时调用）。
// Wave 3 口径：补 error 通道，门拒绝如实上抛，禁止回 (空, false) 伪造"暂无结果"
// （前端 resolve 仍为 [result, found] 数组，语义不变）。
func (s *OcrService) GetSnipResult() (SnipResult, bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return SnipResult{}, false, gateErr
	}
	defer release()
	res := s.cardResult()
	return res, res.Ok, nil
}

// SnipCopyText 手动复制卡片全文（Go 代理写剪贴板，绕开 webview 安全上下文
// 限制），成功后收起卡片。
func (s *OcrService) SnipCopyText() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	res := s.cardResult()
	if strings.TrimSpace(res.Text) == "" {
		return fmt.Errorf("没有可复制的识别文字")
	}
	if err := s.snip.WriteText(res.Text); err != nil {
		return err
	}
	s.SnipCardDismiss()
	return nil
}

// CardDragStart 拖拽把手 mousedown 调用：进入跟手移动会话（重入忽略）。
// Wails beta.10 没有拖拽区 API（v2 SetDragRegion 已移除），原生轮询实现见
// card_windows.go；结束走 CardDragEnd 与左键态检测双通道。
// 纯 void 绑定方法：Wave 3 口径——拒绝即早退，不改签名。
func (s *OcrService) CardDragStart() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.cardMu.Lock()
	card := s.card
	s.cardMu.Unlock()
	s.startCardDrag(card)
}

// CardDragEnd 拖拽把手 mouseup 调用：结束跟手移动会话（幂等）。
// 纯 void 绑定方法：Wave 3 口径——拒绝即早退，不改签名；
// 模块停用中拖拽会话已由窗口侧收口。
func (s *OcrService) CardDragEnd() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.cardDragMu.Lock()
	if s.cardDragStop != nil {
		close(s.cardDragStop)
		s.cardDragStop = nil
	}
	s.cardDragMu.Unlock()
}

// SnipCardDismiss 收起悬浮卡（前端关闭钮/Esc 调用；只隐藏不销毁）。
// 纯 void 绑定方法：Wave 3 口径——拒绝即早退，不改签名。
func (s *OcrService) SnipCardDismiss() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.cardMu.Lock()
	card := s.card
	s.cardMu.Unlock()
	if card != nil {
		card.Hide()
	}
}
