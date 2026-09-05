package quickmenu

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/extapi"
	"hanxi/internal/launcher"
	"hanxi/internal/modules/quickmenu/mousetrap"
	"hanxi/internal/notify"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// 触发与弹窗几何参数（最小验证版先固定，后续可外化到设置页）。
const (
	triggerHold = 450 * time.Millisecond // 右键按住超过该时长触发
	triggerMove = 16                     // 抬手前光标位移容差（物理像素）

	popupWindowName = "quickmenu-popup"
	popupWidth      = 264 // 弹窗尺寸按 DIP 配置（Wails 内部处理 DPI 缩放）
	popupHeight     = 340
)

// QuickMenuService 鼠标快捷菜单：全局右键长按 → 光标处无边框弹窗 → 点击派发条目。
// 条目配置与分发与托盘右键菜单完全共享（settings.TrayMenu + internal/launcher）。
type QuickMenuService struct {
	store    *settings.Store
	registry *extapi.Registry
	disp     *launcher.Dispatcher

	mu      sync.Mutex
	started bool
	mainWin *application.WebviewWindow // route 条目唤主窗用（装配根注入）
	popup   *application.WebviewWindow
	trap    *mousetrap.Trap
}

func NewQuickMenuService(store *settings.Store, registry *extapi.Registry) *QuickMenuService {
	s := &QuickMenuService{store: store, registry: registry}
	s.disp = launcher.New(registry, store, s.navigateMain)
	return s
}

// SetMainWindow 注入主窗口引用（装配根在窗口创建后调用一次）。
func (s *QuickMenuService) SetMainWindow(win *application.WebviewWindow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mainWin = win
}

// ---------- 生命周期（由 module.go 的 OnInit/OnDestroy 驱动） ----------

func (s *QuickMenuService) start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	a := application.Get()
	if a == nil {
		return fmt.Errorf("快捷菜单需要在应用运行后初始化，请重试")
	}

	trap, err := mousetrap.Start(mousetrap.Config{MinHold: triggerHold, MaxMove: triggerMove})
	if err != nil {
		return err
	}

	s.mu.Lock()
	popup := s.popup
	s.mu.Unlock()
	if popup == nil {
		popup = a.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             popupWindowName,
			Title:            "快捷菜单",
			Width:            popupWidth,
			Height:           popupHeight,
			Hidden:           true, // 常驻隐藏待唤，首次触发前不占屏
			Frameless:        true,
			AlwaysOnTop:      true,
			DisableResize:    true,
			Windows:          application.WindowsWindow{HiddenOnTaskbar: true}, // 不进任务栏/Alt+Tab
			URL:              "/#quickmenu",                                    // 前端按 hash 分流挂载弹窗视图（main.ts）
			BackgroundColour: application.NewRGB(245, 246, 248),
		})
		// 关窗/失焦均收起不销毁（beta.10 无公开销毁 API，隐藏复用与会话驻留的托盘隐藏策略同构，
		// 也避免误触"最后窗口"退出分支）。
		popup.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
			ev.Cancel()
			popup.Hide()
		})
		popup.OnWindowEvent(events.Common.WindowLostFocus, func(ev *application.WindowEvent) {
			popup.Hide()
		})
		s.mu.Lock()
		s.popup = popup
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.trap = trap
	s.started = true
	s.mu.Unlock()

	go s.consumeEvents(trap)
	slog.Info("quickmenu: 右键长按唤出已启用", "hold", triggerHold, "moveTol", triggerMove)
	return nil
}

func (s *QuickMenuService) stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	trap := s.trap
	popup := s.popup
	s.trap = nil
	s.mu.Unlock()

	if trap != nil {
		if err := trap.Stop(); err != nil { // Stop 同步等待钩子线程摘钩退出，消费协程随通道关闭自然收尾
			slog.Warn("quickmenu: 钩子停止异常", "err", err)
		}
	}
	if popup != nil {
		// 隐藏驻留而非销毁（beta.10 无公开销毁 API；与 ddns 面板窗口"永不销毁子窗"同策略），
		// 重新启用时经 start() 直接复用，零重建成本。
		popup.Hide()
	}
	slog.Info("quickmenu: 右键长按唤出已停用")
	return nil
}

// consumeEvents 消费钩子的长按触发与真实点击观察（独立协程，绝不在回调线程做 UI
// 操作）；两通道随 Stop 关闭，双双耗尽后自然收尾，不留悬挂协程。
func (s *QuickMenuService) consumeEvents(trap *mousetrap.Trap) {
	triggers, clicks := trap.C(), trap.Buttons()
	for triggers != nil || clicks != nil {
		select {
		case trg, ok := <-triggers:
			if !ok {
				triggers = nil
				continue
			}
			s.showAt(trg)
		case btn, ok := <-clicks:
			if !ok {
				clicks = nil
				continue
			}
			s.dismissIfOutside(btn)
		}
	}
}

// dismissIfOutside 点击弹窗外部区域即收起。这是对失焦收起的兜底：弹窗因 Windows
// 前台锁抢不到焦点时 Wails 的 LostFocus 根本不会触发，只能靠全局点击观察。
// 坐标同用物理像素系（钩子 pt 与 PhysicalBounds），无需换算。
func (s *QuickMenuService) dismissIfOutside(btn mousetrap.ButtonEvent) {
	s.mu.Lock()
	popup := s.popup
	s.mu.Unlock()
	if popup == nil || !popup.IsVisible() {
		return
	}
	b := popup.PhysicalBounds()
	if int32(b.X) <= btn.X && btn.X < int32(b.X+b.Width) &&
		int32(b.Y) <= btn.Y && btn.Y < int32(b.Y+b.Height) {
		return // 点在菜单上：留给 WebView2 自己的条目点击处理
	}
	popup.Hide()
}

// showAt 将弹窗定位到光标处并置前。坐标换算：钩子给的是物理像素，
// Wails 窗口 API 按 DIP 工作，经 ScreenManager 换算并以就近显示器工作区钳位，
// 保证弹窗在屏幕边缘/多显示器下不被裁掉。
func (s *QuickMenuService) showAt(trg mousetrap.Trigger) {
	a := application.Get()
	if a == nil || a.Screen == nil {
		return
	}

	s.mu.Lock()
	popup := s.popup
	s.mu.Unlock()
	if popup == nil {
		return
	}

	physical := application.Point{X: int(trg.X), Y: int(trg.Y)}
	dip := a.Screen.PhysicalToDipPoint(physical)
	x, y := dip.X, dip.Y

	w, h := popup.Size()
	if scr := a.Screen.ScreenNearestPhysicalPoint(physical); scr != nil {
		wa := scr.WorkArea
		if x+w > wa.X+wa.Width {
			x = wa.X + wa.Width - w
		}
		if y+h > wa.Y+wa.Height {
			y = wa.Y + wa.Height - h
		}
		if x < wa.X {
			x = wa.X
		}
		if y < wa.Y {
			y = wa.Y
		}
	}

	popup.SetPosition(x, y)
	popup.Show()
	popup.Focus()
	// Wails Focus 是裸 SetForegroundWindow：本进程处于后台（主窗在托盘）时会被
	// Windows 前台锁拒绝，弹窗拿不到焦点则 Esc/失焦收起失灵——借用前台窗口线程
	// 输入特权强制置前。
	if err := windows.SetForegroundForce(uintptr(popup.NativeWindow())); err != nil {
		slog.Debug("quickmenu: 强制置前失败（依赖点击外部兜底收起）", "err", err)
	}
	// 通知弹窗视图重拉条目（托盘配置可能已在设置页改过，弹窗常驻不重启）
	a.Event.Emit("quickmenu:opening")
}

// navigateMain route 条目动作：显示主窗口并请求前端导航（与托盘 route 条目同构）。
func (s *QuickMenuService) navigateMain(route string) {
	s.mu.Lock()
	win := s.mainWin
	s.mu.Unlock()
	if a := application.Get(); a != nil && a.Event != nil {
		a.Event.Emit("tray:navigate", route)
	}
	if win != nil {
		win.Show()
		win.Focus()
	}
}

// ---------- 前端绑定 API ----------

// GetStatus 返回快捷菜单运行态（模块页展示）。
func (s *QuickMenuService) GetStatus() Status {
	items := s.disp.EnabledItems()
	return Status{
		TrapActive: s.trapActive(),
		HoldMs:     int(triggerHold / time.Millisecond),
		MoveTol:    triggerMove,
		ItemCount:  len(items),
	}
}

// ListItems 返回弹窗菜单条目（复用托盘配置中启用的条目，展示序即索引序）。
func (s *QuickMenuService) ListItems() []MenuItem {
	enabled := s.disp.EnabledItems()
	out := make([]MenuItem, 0, len(enabled))
	for i, item := range enabled {
		hint := item.Ref
		if item.Type == settings.TrayItemExe {
			hint = item.Path
		}
		out = append(out, MenuItem{
			Index: i,
			Label: s.disp.Label(item),
			Type:  item.Type,
			Hint:  hint,
		})
	}
	return out
}

// Launch 执行第 index 个条目：先收起弹窗给即时反馈，派发进 goroutine，
// 失败统一走通知 Hub（与托盘失败反馈同构）。
func (s *QuickMenuService) Launch(index int) error {
	items := s.disp.EnabledItems()
	if index < 0 || index >= len(items) {
		return fmt.Errorf("菜单条目不存在（索引 %d）", index)
	}
	item := items[index]

	s.Dismiss()
	go func() {
		if err := s.disp.Dispatch(context.Background(), item); err != nil {
			slog.Warn("quickmenu: launch failed", "type", item.Type, "ref", item.Ref, "err", err)
			notify.GetHub().Emit(&notify.Notification{
				ModuleID: "quickmenu",
				Title:    "快捷菜单执行失败",
				Message:  fmt.Sprintf("%s：%v", s.disp.Label(item), err),
				Level:    notify.LevelError,
			})
		}
	}()
	return nil
}

// OpenSettings 引导至设置页托盘菜单配置区（弹窗空态的"去配置"动作），并收起弹窗。
func (s *QuickMenuService) OpenSettings() {
	s.navigateMain("/settings")
	s.Dismiss()
}

// Dismiss 收起弹窗（前端 Esc / 空背景点击调用）。
func (s *QuickMenuService) Dismiss() {
	s.mu.Lock()
	popup := s.popup
	s.mu.Unlock()
	if popup != nil {
		popup.Hide()
	}
}

func (s *QuickMenuService) trapActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && s.trap != nil
}
