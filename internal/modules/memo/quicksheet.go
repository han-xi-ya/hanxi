package memo

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/platform/windows"
)

// 悬浮速记卡（N16 B 批核心件）：全局热键随处唤出的极小速记窗，落笔即存、
// 失焦即收。窗口形态与 quickmenu 轮盘/msgboard 留言牌同谱（本仓 beta.10
// 多窗实证模板）：frameless 真透明 + 置顶 + 不进任务栏/Alt+Tab，建窗即隐藏、
// 摆位完成后一次性露出防半帧闪；收起空闲 TTL 后真销毁（#53 摘 hook→Close
// 通路，不养白烧的 WebView2 视图），再唤重建只付一次建窗延迟。
//
// 数据面零新轨：卡内保存直接走既有 Create RPC（memo:changed 广播让主窗列表
// 实时跟新）；IsMasked 语义不受影响——速记恒为非敏感，页面不提供遮罩开关。
//
// 锁纪律：s.mu 管便签数据；速记卡侧一切记账（窗口引用、显隐、热键注册器、
// started）归 s.sheetMu，且 Wails 窗口 API（内部主线程 InvokeSync）一律在
// 锁外调用，防锁反转（msgboard 同款）。

const (
	sheetWindowName = "memo-quicksheet"
	sheetWindowURL  = "/#memosheet"
	sheetOpeningEv  = "memo:quicksheet:opening" // 无载荷 Void 事件（注册见 app.go）
	sheetIdleTTL    = 2 * time.Minute           // 收起后空闲销毁时限：速记卡比轮盘冷，留更短
	sheetWidth      = 620                       // DIP：与主窗速记条同宽容纳一行短句
	sheetHeight     = 196                       // DIP：输入条 + 标签条 + 提示行
	sheetEdgeMargin = 16                        // 卡体四周透明边距：容纳页面自绘投影（#50 纪律）
	sheetTopPercent = 18                        // 工作区高度百分比落点：偏上不遮视线焦点区
)

// showSheet 唤出速记卡：取窗（隐藏驻留热复用 / 已销毁或从未建则按需新建）→
// 摆位（跟随光标所在显示器工作区）→ Show+Focus+强制置前 → 广播 opening 让
// 页面清稿聚焦。应用未运行（无头单测/装配前）返回可读错误。
func (s *MemoService) showSheet() error {
	a := application.Get()
	if a == nil || a.Window == nil {
		return fmt.Errorf("悬浮速记卡需要在应用运行后唤出，请稍后重试")
	}

	s.sheetMu.Lock()
	if !s.sheetStarted {
		s.sheetMu.Unlock()
		return nil // stop 接管期间的迟到回调：静默不复活
	}
	s.sheetShown = true
	if s.sheetIdle != nil {
		s.sheetIdle.Stop()
		s.sheetIdle = nil
	}
	win := s.sheetWin
	s.sheetMu.Unlock()

	if win == nil {
		win = s.createSheet()
		if win == nil {
			s.sheetMu.Lock()
			s.sheetShown = false
			s.sheetMu.Unlock()
			return fmt.Errorf("悬浮速记卡创建失败，请稍后重试")
		}
	}

	s.placeSheet(a, win)
	win.Show()
	win.Focus()
	// Wails Focus 是裸 SetForegroundWindow：主窗藏托盘等后台态会被前台锁拒绝，
	// 卡拿不到焦点则回车/Esc 全失灵——quickmenu/msgboard 同款借权强制置前。
	if err := windows.SetForegroundForce(uintptr(win.NativeWindow())); err != nil {
		slog.Debug("memo: 速记卡强制置前失败（依赖页面内操作或再次热键收起）", "err", err)
	}
	if a.Event != nil {
		a.Event.Emit(sheetOpeningEv) // 页面据此清空残稿并聚焦输入条
	}

	// 提交前复核（msgboard 建窗期停用竞态同款纪律的轻量版）：stop 可能恰在
	// started 检查与建窗之间完成——失效结果必须由本次 Show 自行真销毁，
	// 不给已停用模块留僵尸窗。
	s.sheetMu.Lock()
	stale := !s.sheetStarted
	if stale {
		s.sheetShown = false
	}
	s.sheetMu.Unlock()
	if stale {
		s.destroySheet(false)
		return nil
	}
	return nil
}

// createSheet 按需建卡（仅 showSheet 调用，单点串行免竞态）。
// WindowClosing 拦截为"收起不销毁"挡住 Alt+F4 误杀（拦截丢内容属预期：
// 卡是快捕面不是编辑器，正文未回车即未入库）；真销毁走 destroySheet。
func (s *MemoService) createSheet() *application.WebviewWindow {
	a := application.Get()
	if a == nil || a.Window == nil {
		return nil
	}
	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             sheetWindowName,
		Title:            "速记",
		Width:            sheetWidth,
		Height:           sheetHeight,
		Hidden:           true, // 建窗即隐藏，摆位完成后一次性露出
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundType:   application.BackgroundTypeTransparent,            // 真透明：卡体由页面自绘（#50）
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true}, // 不进任务栏/Alt+Tab
		URL:              sheetWindowURL,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	offClosing := win.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		ev.Cancel()
		s.hideSheet() // 窗体事件走无门内部版（事件线程不属 RPC 面）
	})
	// 失焦即收：速记卡用完就走，常驻焦点是干扰。抢不到前台焦点时 LostFocus
	// 根本不会触发（前台锁），与轮盘同态——依赖再次热键/Alt+F4 出口即可。
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		s.hideSheet()
	})

	s.sheetMu.Lock()
	if s.sheetWin != nil { // 防御：同名窗理论上不并存，出现即先拆旧账再挂新
		old, oldOff := s.sheetWin, s.sheetClosing
		s.sheetMu.Unlock()
		if oldOff != nil {
			oldOff()
		}
		old.Close()
		s.sheetMu.Lock()
	}
	s.sheetWin, s.sheetClosing = win, offClosing
	s.sheetMu.Unlock()
	return win
}

// placeSheet 把卡摆到光标所在显示器工作区的水平居中偏上位：速记的动线是
// "手停在键盘、眼停在屏幕中央"，卡出现在工作区上三分之一不打断当前视线。
// 显示器判定链：GetCursorPos 物理点 → 最近屏 → 工作区 DIP 纯几何；任一环
// 失效（无 Screen 管理器/显示器失位）回落 Wails 零值几何（自动居中）。
func (s *MemoService) placeSheet(a *application.App, win *application.WebviewWindow) {
	if a.Screen == nil {
		return
	}
	var scr *application.Screen
	if cx, cy, err := windows.CursorPos(); err == nil {
		scr = a.Screen.ScreenNearestPhysicalPoint(application.Point{X: cx, Y: cy})
	}
	if scr == nil {
		for _, cand := range a.Screen.GetAll() {
			if cand != nil && cand.IsPrimary {
				scr = cand
				break
			}
		}
	}
	if scr == nil {
		if all := a.Screen.GetAll(); len(all) > 0 {
			scr = all[0]
		}
	}
	if scr == nil {
		return
	}
	wa := scr.WorkArea
	x, y := planSheetPlacement(wa.X, wa.Y, wa.Width, wa.Height, sheetWidth, sheetHeight)
	win.SetPosition(x, y)
}

// planSheetPlacement 工作区内摆位纯函数（单测锁语义）：水平居中、垂直落
// sheetTopFraction（工作区高度 18%）处，并钳在"整窗不出工作区"范围内；
// 窗口大于工作区时贴工作区左上，绝不产生负偏移出屏。
func planSheetPlacement(waX, waY, waW, waH, winW, winH int) (int, int) {
	x, y := waX, waY
	if winW < waW {
		x = waX + (waW-winW)/2
	}
	if winH < waH {
		y = waY + waH*sheetTopPercent/100
		if y+winH > waY+waH {
			y = waY + waH - winH
		}
	}
	return x, y
}

// hideSheet 收起卡并武装空闲销毁：TTL 内再唤热复用，超时释放 WebView2 内存。
func (s *MemoService) hideSheet() {
	s.sheetMu.Lock()
	win := s.sheetWin
	s.sheetShown = false
	if s.sheetIdle != nil {
		s.sheetIdle.Stop()
	}
	s.sheetIdle = time.AfterFunc(sheetIdleTTL, func() { s.destroySheet(true) })
	s.sheetMu.Unlock()
	if win != nil {
		win.Hide()
	}
}

// destroySheet 真销毁卡并清账（下次唤出走 createSheet 重建）。
// skipIfShown=true（空闲计时器路径）：用户正在用则跳过；false（模块停用路径）：
// 无论显隐一律释放。#53 通路：先摘 Closing 拦截 hook 再 Close 才走 Wails 内部
// 销毁（markAsDestroyed + 从窗口管理器除名），beta.10 无公开 Destroy()。
func (s *MemoService) destroySheet(skipIfShown bool) {
	s.sheetMu.Lock()
	if s.sheetIdle != nil {
		s.sheetIdle.Stop()
		s.sheetIdle = nil
	}
	if s.sheetWin == nil || (skipIfShown && s.sheetShown) {
		s.sheetMu.Unlock()
		return
	}
	win, off := s.sheetWin, s.sheetClosing
	s.sheetWin, s.sheetClosing, s.sheetShown = nil, nil, false
	s.sheetMu.Unlock()

	if off != nil {
		off()
	}
	win.Close()
	slog.Debug("memo: 悬浮速记卡已销毁（WebView2 视图内存释放）")
}

// toggleQuickSheet 无门内部版互切：热键回调直调（回调线程不属 RPC 面）。
func (s *MemoService) toggleQuickSheet() error {
	s.sheetMu.Lock()
	shown := s.sheetShown
	s.sheetMu.Unlock()
	if shown {
		s.hideSheet()
		return nil
	}
	return s.showSheet()
}

// ---------- RPC 导出版（统一调用门，Wave 3 口径） ----------

// ToggleQuickSheet 速记卡唤出↔收起互切（RPC 导出版：接统一调用门）。
func (s *MemoService) ToggleQuickSheet() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.toggleQuickSheet()
}

// ShowQuickSheet 唤出速记卡（模块页按钮直达；已显示时幂等重摆位抢焦点）。
func (s *MemoService) ShowQuickSheet() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.showSheet()
}

// HideQuickSheet 收起速记卡（卡内 Esc 出口；幂等，未显示时静默成功）。
func (s *MemoService) HideQuickSheet() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.hideSheet()
	return nil
}

// GetQuickSheetState 热键配置与系统实况回显（模块页设置区一次拉全）。
// 在位与否问注册器槽位实况（底层即 manager.IsRegistered）而非自记状态——
// 开机期抢键失败只降级不报错，页面据此提示改键或走按钮唤出（口径同 msgboard）。
func (s *MemoService) GetQuickSheetState() (QuickSheetState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuickSheetState{}, gateErr
	}
	defer release()

	r := s.hotkeyRegistry()
	return QuickSheetState{
		Hotkey:       s.prefs.Hotkey(),
		HotkeyActive: r != nil && r.Registered(quickHotkeySlot),
	}, nil
}

// SetQuickSheetHotkey 改速记热键（RPC 导出版：接统一调用门）。事务语义见
// setQuickHotkey：冲突时旧键全程活着、配置不动，中文错误上抛由页面红字呈现。
func (s *MemoService) SetQuickSheetHotkey(raw string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.setQuickHotkey(raw)
}
