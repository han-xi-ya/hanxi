package clipboard

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	hw "hanxi/internal/platform/windows"
)

// 剪贴板浮层（契约 §6）：全局热键随处唤出的历史选择卡，形态与 memo 速记卡/
// quickmenu 轮盘/msgboard 留言牌同谱（beta.10 多窗实证模板）——frameless 真透明
// + 置顶 + 不进任务栏/Alt+Tab，建窗即隐藏、摆位完成后一次性露出防半帧闪；
// 收起空闲 TTL 后真销毁（#53：摘 WindowClosing hook 再 Close 才走内部销毁通路，
// 不养白烧的 WebView2 视图），再唤重建只付一次建窗延迟。
//
// 锁纪律：浮层一切簿记（窗口引用/显隐/空闲计时）归 s.ovMu，且 Wails 窗口 API
// （内部主线程 InvokeSync）一律在锁外调用，防锁反转（memo 速记卡同款）。

const (
	overlayWindowName = "clipboard-overlay"         // 窗名冻结（main.ts 接线按此路由）
	overlayWindowURL  = "/#clipboardoverlay"        // hash 路由（A5 视图按此判定）
	overlayOpeningEv  = "clipboard:overlay:opening" // 清稿 Void 事件（前端常量同名）
	overlayIdleTTL    = 2 * time.Minute             // 收起后空闲销毁时限（契约 §6）

	overlayWidth      = 640 // DIP：列表卡宽度，容纳 Preview+来源行
	overlayHeight     = 520 // DIP：搜索条 + 约十行列表 + 页脚状态
	overlayEdgeMargin = 16  // 卡体四周透明边距：容纳页面自绘投影（#50 纪律）
	overlayTopPercent = 18  // 工作区高度百分比落点：偏上不遮视线焦点区
)

// showOverlay 唤出浮层：取窗（未销毁则热复用 / 已销毁或从未建则按需新建）→
// 摆位（跟随光标所在显示器工作区）→ Show+Focus+强制置前 → 广播 opening 让
// 页面清稿。应用未运行（无头单测/装配前）返回可读错误，不 panic。
func (s *ClipboardService) showOverlay() error {
	a := application.Get()
	if a == nil || a.Window == nil {
		return fmt.Errorf("剪贴板浮层需要在应用运行后唤出，请稍后重试")
	}

	if !s.isStarted() {
		return nil // stop 接管期间的迟到回调：静默不复活
	}
	s.ovMu.Lock()
	s.ovShown = true
	if s.ovIdle != nil {
		s.ovIdle.Stop()
		s.ovIdle = nil
	}
	win := s.ovWin
	s.ovMu.Unlock()

	if win == nil {
		win = s.createOverlay()
		if win == nil {
			s.ovMu.Lock()
			s.ovShown = false
			s.ovMu.Unlock()
			return fmt.Errorf("剪贴板浮层创建失败，请稍后重试")
		}
	}

	s.placeOverlay(a, win)
	win.Show()
	win.Focus()
	// Wails Focus 是裸 SetForegroundWindow：主窗藏托盘等后台态会被前台锁拒绝，
	// 浮层拿不到焦点则回车选取/Esc 全失灵——速记卡/轮盘同款借权强制置前。
	if err := hw.SetForegroundForce(uintptr(win.NativeWindow())); err != nil {
		slog.Debug("clipboard: 浮层强制置前失败（依赖页面内操作或再次热键收起）", "err", err)
	}
	if a.Event != nil {
		a.Event.Emit(overlayOpeningEv) // 页面据此清稿聚焦搜索条
	}

	// 提交前复核（memo 建窗期停用竞态同款纪律的轻量版）：stop 可能恰在
	// started 检查与建窗之间完成——失效结果必须由本次 Show 自行真销毁，
	// 不给已停用模块留僵尸窗。两把锁顺序取用不嵌套（ovMu/s.mu 无交叉序）。
	if !s.isStarted() {
		s.ovMu.Lock()
		s.ovShown = false
		s.ovMu.Unlock()
		s.destroyOverlay(false)
	}
	return nil
}

// isStarted 生命周期在场判定（OnInit~OnDestroy 窗口期）。
func (s *ClipboardService) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// createOverlay 按需建卡（仅 showOverlay 调用，单点串行免竞态）。
// WindowClosing 拦截为"收起不销毁"挡住 Alt+F4 误杀；真销毁走 destroyOverlay。
func (s *ClipboardService) createOverlay() *application.WebviewWindow {
	a := application.Get()
	if a == nil || a.Window == nil {
		return nil
	}
	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             overlayWindowName,
		Title:            "剪贴板",
		Width:            overlayWidth,
		Height:           overlayHeight,
		Hidden:           true, // 建窗即隐藏，摆位完成后一次性露出
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundType:   application.BackgroundTypeTransparent,            // 真透明：卡体由页面自绘（#50）
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true}, // 不进任务栏/Alt+Tab
		URL:              overlayWindowURL,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	offClosing := win.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		ev.Cancel()
		s.hideOverlay() // 窗体事件走无门内部版（事件线程不属 RPC 面）
	})
	// 失焦即收：选取用完就走，常驻焦点是干扰。抢不到前台焦点时 LostFocus
	// 根本不会触发（前台锁），与速记卡同态——依赖再次热键/Alt+F4 出口即可。
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		s.hideOverlay()
	})

	s.ovMu.Lock()
	if s.ovWin != nil { // 防御：同名窗理论上不并存，出现即先拆旧账再挂新
		old, oldOff := s.ovWin, s.ovClosing
		s.ovMu.Unlock()
		if oldOff != nil {
			oldOff()
		}
		old.Close()
		s.ovMu.Lock()
	}
	s.ovWin, s.ovClosing = win, offClosing
	s.ovMu.Unlock()
	return win
}

// placeOverlay 把浮层摆到光标所在显示器工作区的水平居中偏上位：唤出动作线是
// "手停在键盘、眼停在屏幕中央"，卡出现在工作区上三分之一不打断当前视线。
// 显示器判定链：GetCursorPos 物理点 → 最近屏 → 工作区 DIP 纯几何；任一环
// 失效（无 Screen 管理器/显示器失位）回落 Wails 零值几何（自动居中）。
func (s *ClipboardService) placeOverlay(a *application.App, win *application.WebviewWindow) {
	if a.Screen == nil {
		return
	}
	var scr *application.Screen
	if cx, cy, err := hw.CursorPos(); err == nil {
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
	x, y := planOverlayPlacement(wa.X, wa.Y, wa.Width, wa.Height, overlayWidth, overlayHeight)
	win.SetPosition(x, y)
}

// planOverlayPlacement 工作区内摆位纯函数（单测锁语义）：水平居中、垂直落
// 工作区高度 overlayTopPercent 处，并钳在"整窗不出工作区"范围内；
// 窗口大于工作区时贴工作区左上，绝不产生负偏移出屏。
func planOverlayPlacement(waX, waY, waW, waH, winW, winH int) (int, int) {
	x, y := waX, waY
	if winW < waW {
		x = waX + (waW-winW)/2
	}
	if winH < waH {
		y = waY + waH*overlayTopPercent/100
		if y+winH > waY+waH {
			y = waY + waH - winH
		}
	}
	return x, y
}

// hideOverlay 收起浮层并武装空闲销毁：TTL 内再唤热复用，超时释放 WebView2 内存。
func (s *ClipboardService) hideOverlay() {
	s.ovMu.Lock()
	win := s.ovWin
	s.ovShown = false
	if s.ovIdle != nil {
		s.ovIdle.Stop()
	}
	s.ovIdle = time.AfterFunc(overlayIdleTTL, func() { s.destroyOverlay(true) })
	s.ovMu.Unlock()
	if win != nil {
		win.Hide()
	}
}

// destroyOverlay 真销毁浮层并清账（下次唤出走 createOverlay 重建）。
// skipIfShown=true（空闲计时器路径）：用户正在用则跳过；false（模块停用路径）：
// 无论显隐一律释放。#53 通路：先摘 Closing 拦截 hook 再 Close 才走 Wails 内部
// 销毁（markAsDestroyed + 从窗口管理器除名），beta.10 无公开 Destroy()。
func (s *ClipboardService) destroyOverlay(skipIfShown bool) {
	s.ovMu.Lock()
	if s.ovIdle != nil {
		s.ovIdle.Stop()
		s.ovIdle = nil
	}
	if s.ovWin == nil || (skipIfShown && s.ovShown) {
		s.ovMu.Unlock()
		return
	}
	win, off := s.ovWin, s.ovClosing
	s.ovWin, s.ovClosing, s.ovShown = nil, nil, false
	s.ovMu.Unlock()

	if off != nil {
		off()
	}
	win.Close()
	slog.Debug("clipboard: 剪贴板浮层已销毁（WebView2 视图内存释放）")
}

// ---------- RPC 导出版（统一调用门，Wave 3 口径） ----------

// CollapseOverlay 浮层显式收窗（契约 §12 v1.4 H1：§4 十法之一）。前端
// input.blur()/window.blur() 唤不动顶层窗 HWND 失活，选取完成/Esc 出口必须
// 走这条显式 RPC——QuickMemoSheet HideQuickSheet 同先例。幂等：未显示时
// 静默成功；内部走无门版 hideOverlay。
func (s *ClipboardService) CollapseOverlay() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.hideOverlay()
	return nil
}

// toggleOverlay 互切显隐：热键回调直调（回调线程不属 RPC 面，不接调用门）。
func (s *ClipboardService) toggleOverlay() error {
	s.ovMu.Lock()
	shown := s.ovShown
	s.ovMu.Unlock()
	if shown {
		s.hideOverlay()
		return nil
	}
	return s.showOverlay()
}
