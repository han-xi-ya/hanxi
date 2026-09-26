package quickmenu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

// 触发参数出厂默认与合法域（N5-C2 外化：用户可配，但盘值按不可信输入处理——
// 出域一律钳回，绝不让坏配置武装鼠标钩子）。几何常量仍为编译期。
const (
	factoryHoldMs, minHoldMs, maxHoldMs = 450, 200, 1500 // 按住触发时长 ms：短于 200 普通右键误触、长于 1.5s 手感死等
	factoryMovePx, minMovePx, maxMovePx = 16, 4, 64      // 位移容差 px：小于 4 抖动误判、大于 64 长按手势漂移失控
)

// effectiveTrigger 读盘值并钳制为当前有效参数（0=出厂默认）。纯函数形态便于表测。
func effectiveTrigger(rawHoldMs, rawMovePx int) (time.Duration, int) {
	hold := clampInt(rawHoldMs, factoryHoldMs, minHoldMs, maxHoldMs)
	move := clampInt(rawMovePx, factoryMovePx, minMovePx, maxMovePx)
	return time.Duration(hold) * time.Millisecond, move
}

func clampInt(v, def, lo, hi int) int {
	if v <= 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// 弹窗几何参数（与前端 wheelGeometry.ts 同源，改动两处需同步——编译期常量，不属外化范围）。
const (
	popupWindowName = "quickmenu-popup"
	// 弹窗收起后的空闲驻留时长：到期真销毁窗口释放 WebView2 内存（下次唤出重建，
	// 代价数百毫秒）；TTL 内再唤出走热复用，零延迟。常驻隐藏换内存的折中点，
	// 轮盘作为高频手势工具，5 分钟覆盖"连用几次"的会话粒度。
	popupIdleTTL = 5 * time.Minute
	// 轮盘弹窗为正方形真透明窗口（BackgroundTypeTransparent，DirectComposition
	// 合成）：主盘直径 340 DIP（r=170）保持不变；分组展开的"外扩子环帽带"画到
	// r=236，故窗口放大为 512 DIP 见方，四周透明边距 popupMargin=20 容纳投影与
	// 外甩取消判定环（r236→244），GDI 区域裁剪只负责把四角从命中测试里剪掉
	// （裁剪圈落在投影淡出后的全透明区，硬边不可见），圆盘视觉边缘全部由页面
	// 抗锯齿绘制。尺寸按 DIP（Wails 处理缩放）。前端 QuickMenuPopup.vue 的
	// wheelGeometry 常量必须与本组数值同源，改动两处需同步。
	popupWidth  = 512
	popupHeight = 512
	popupMargin = 20 // 盘缘外透明边距（DIP）：收起点击判定半径 = 半窗 - 边距 = 子环帽带外缘
)

// QuickMenuService 鼠标快捷菜单：全局右键长按 → 光标处弹出圆盘 → 点击扇区派发条目。
// 条目配置与分发与托盘右键菜单完全共享（settings.TrayMenu + internal/launcher）；
// group 分组条目在二级轮盘开启时悬停在外扩子环（StarPie 式级联外扩，见前端
// QuickMenuPopup.vue 与 wheelGeometry.ts），关闭时子条目拍平进主盘，
// 展示与派发共用 wheelView 保证索引一致。
type QuickMenuService struct {
	store    *settings.Store
	registry *extapi.Registry
	disp     *launcher.Dispatcher

	mu         sync.Mutex
	started    bool
	clipWarned bool                       // 裁剪失败已告警过（每次唤出都裁，只首报防刷屏）
	mainWin    *application.WebviewWindow // route 条目唤主窗用（装配根注入）
	popup      *application.WebviewWindow
	// 弹窗生命周期三态：popup 非空=窗体存在（可见或隐藏驻留）；
	// popupShown 与 show/hide 调用严格同步——显隐判定走状态机而非
	// IsVisible（后者是主线程 InvokeSync，持锁期间调用有锁反转风险）。
	popupShown   bool
	popupClosing func() // WindowClosing 拦截 hook 的注销闭包（销毁前摘除，放行 Close 走 Wails 内部销毁路径）
	popupIdle    *time.Timer
	trap         *mousetrap.Trap
	holder       *extapi.LeaseHolder
}

// NewQuickMenuService 装配常驻单例服务：条目派发器复用 internal/launcher（与托盘菜单同语义），
// navigateMain 回调用于 route 条目唤起主窗口；构造无 IO，钩子与弹窗由 start 懒建。
// RPC 导出版方法经 holder 接入统一调用门（Wave 3）；钩子协程（consumeEvents →
// showAt/dismissIfOutside）与窗体事件（Closing/LostFocus → hidePopup）走内部无门版。
func NewQuickMenuService(store *settings.Store, registry *extapi.Registry, holder *extapi.LeaseHolder) *QuickMenuService {
	s := &QuickMenuService{store: store, registry: registry, holder: holder}
	s.disp = launcher.New(registry, store, s.navigateMain)
	return s
}

// SetMainWindow 注入主窗口引用（装配根在窗口创建后调用一次）。
// 装配布线: Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)——不接调用门。
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

	if application.Get() == nil {
		return fmt.Errorf("快捷菜单需要在应用运行后初始化，请重试")
	}

	hold, move := s.effectiveTrigger()
	trap, err := mousetrap.Start(mousetrap.Config{MinHold: hold, MaxMove: int32(move)})
	if err != nil {
		return err
	}

	// 弹窗不在启动时预建：首次唤出才创建（showAt → acquirePopup → createPopup），
	// 收起空闲 popupIdleTTL 后自动销毁。开机常驻只养一个从不露面的 WebView2 视图，
	// 实测白占几十 MB——按需创建把这笔固定税还给系统。
	s.mu.Lock()
	s.trap = trap
	s.started = true
	s.mu.Unlock()

	go s.consumeEvents(trap)
	slog.Info("quickmenu: 右键长按唤出已启用", "hold", hold, "moveTol", move)
	return nil
}

// effectiveTrigger 当前生效触发参数（store 缺失回出厂值）。
func (s *QuickMenuService) effectiveTrigger() (time.Duration, int) {
	if s.store == nil {
		return time.Duration(factoryHoldMs) * time.Millisecond, factoryMovePx
	}
	return effectiveTrigger(s.store.GetQuickMenuTrigger())
}

// createPopup 按需创建轮盘弹窗（调用方只有 consumeEvents 协程的 acquirePopup，
// 单点串行天然免竞态；其余读方经 s.mu）。建窗即隐藏，随后 SetPosition+Show 展示。
//
// WindowClosing 默认被拦截为"收起不销毁"（Cancel+Hide），挡住 Alt+F4 误杀；
// 空闲销毁见 destroyPopup：先注销该 hook 再 Close，事件不再被拦截，Wails 内部
// WindowClosing 监听器执行真销毁（markAsDestroyed + chromium.ShuttingDown + 从
// 窗口管理器除名），WebView2 视图与页面内存真正归还，同名窗口此后可再重建。
// beta.10 无公开 Destroy()，这是唯一销毁路径（详见 docs/TROUBLESHOOTING.md）。
func (s *QuickMenuService) createPopup() *application.WebviewWindow {
	a := application.Get()
	if a == nil || a.Window == nil {
		return nil
	}
	popup := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             popupWindowName,
		Title:            "快捷菜单",
		Width:            popupWidth,
		Height:           popupHeight,
		Hidden:           true, // 建窗即隐藏，showAt 定位完成后立即 Show
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundType:   application.BackgroundTypeTransparent,            // 真透明：圆盘边缘抗锯齿由页面绘制，杜绝窗底白边
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true}, // 不进任务栏/Alt+Tab
		URL:              "/#quickmenu",                                    // 前端按 hash 分流挂载弹窗视图（main.ts）
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	offClosing := popup.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		ev.Cancel()
		s.hidePopup()
	})
	popup.OnWindowEvent(events.Common.WindowLostFocus, func(ev *application.WindowEvent) {
		s.hidePopup()
	})
	// 跨缩放比屏幕时 Wails 按建议物理矩形直接改窗（DIP 恒定会变成像素恒定的
	// 512/640/768…），GDI 裁剪圈不会随动——DPI 变化后兜底重裁。
	popup.OnWindowEvent(events.Common.WindowDPIChanged, func(ev *application.WindowEvent) {
		s.clipPopup(popup)
	})
	s.mu.Lock()
	s.popup = popup
	s.popupClosing = offClosing
	s.popupShown = false
	s.mu.Unlock()
	return popup
}

// acquirePopup 取一个可展示的弹窗并标记为使用中：隐藏驻留的取消空闲销毁计时热复用；
// 不存在（从未建或已空闲销毁）则按需新建。仅当应用实例不可用时返回 nil。
func (s *QuickMenuService) acquirePopup() *application.WebviewWindow {
	s.mu.Lock()
	if s.popup == nil {
		s.mu.Unlock()
		popup := s.createPopup() // 仅 consumeEvents 协程调用，内部串行安装到 s.popup
		if popup == nil {
			return nil
		}
		s.mu.Lock()
		s.popupShown = true // 新建路径同样标记在用，首显期间的点外收起兜底才生效
		s.mu.Unlock()
		return popup
	}
	popup := s.popup
	if s.popupIdle != nil {
		s.popupIdle.Stop()
		s.popupIdle = nil
	}
	s.popupShown = true
	s.mu.Unlock()
	return popup
}

// hidePopup 收起轮盘并武装空闲销毁计时：popupIdleTTL 内无人再唤即释放整窗内存。
func (s *QuickMenuService) hidePopup() {
	s.mu.Lock()
	popup := s.popup
	s.popupShown = false
	if s.popupIdle != nil {
		s.popupIdle.Stop()
	}
	s.popupIdle = time.AfterFunc(popupIdleTTL, func() { s.destroyPopup(true) })
	s.mu.Unlock()
	if popup != nil {
		popup.Hide()
	}
}

// destroyPopup 真销毁弹窗并清空引用，下次唤出走 createPopup 重建（代价是一次轮盘
// 延迟出现）。skipIfShown=true（空闲计时器路径）：用户正在用则跳过，收起时自会
// 重新武装；false（模块停用路径）：无论显隐一律释放。
func (s *QuickMenuService) destroyPopup(skipIfShown bool) {
	s.mu.Lock()
	if s.popupIdle != nil {
		s.popupIdle.Stop()
		s.popupIdle = nil
	}
	if s.popup == nil || (skipIfShown && s.popupShown) {
		s.mu.Unlock()
		return
	}
	popup, off := s.popup, s.popupClosing
	s.popup, s.popupClosing, s.popupShown = nil, nil, false
	s.mu.Unlock()

	if off != nil {
		off() // 摘除"关即收起"拦截，让下面的 Close 放行到 Wails 内部销毁路径
	}
	popup.Close()
	slog.Debug("quickmenu: 轮盘弹窗已销毁（WebView2 视图内存释放）")
}

func (s *QuickMenuService) stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	trap := s.trap
	s.trap = nil
	s.mu.Unlock()

	if trap != nil {
		if err := trap.Stop(); err != nil { // Stop 同步等待钩子线程摘钩退出，消费协程随通道关闭自然收尾
			slog.Warn("quickmenu: 钩子停止异常", "err", err)
		}
	}
	// 停用即销毁：模块关了就释放弹窗占的 WebView2 内存，重新启用后首次唤出重建。
	s.destroyPopup(false)
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

// dismissIfOutside 点击圆盘之外即收起。这是对失焦收起的兜底：弹窗因 Windows
// 前台锁抢不到焦点时 Wails 的 LostFocus 根本不会触发，只能靠全局点击观察。
// 命中判定按圆而非矩形：窗口已被区域裁剪，方形四角本就不属于轮盘视觉。
// 坐标同用物理像素系（钩子 pt 与 PhysicalBounds），无需换算。
func (s *QuickMenuService) dismissIfOutside(btn mousetrap.ButtonEvent) {
	s.mu.Lock()
	popup, shown := s.popup, s.popupShown
	s.mu.Unlock()
	if popup == nil || !shown {
		return
	}
	b := popup.PhysicalBounds()
	cx := b.X + b.Width/2
	cy := b.Y + b.Height/2
	dx := int(btn.X) - cx
	dy := int(btn.Y) - cy
	// 判定半径取盘面而非窗口半宽：盘缘外的透明投影边距会被 WebView 吃掉点击，
	// 若按窗口半径算，点投影圈会"无事发生"——按盘半径则即时收起，符合直觉。
	scale := float64(b.Width) / float64(popupWidth)
	r := float64(b.Width)/2 - float64(popupMargin)*scale
	if float64(dx*dx+dy*dy) <= r*r {
		return // 点在圆盘上：留给 WebView2 自己的扇区点击处理
	}
	s.hidePopup()
}

// showAt 将圆盘中心对准光标并置前（轮盘可辨识度依赖"盘心=光标"的肌肉记忆，
// 不能沿用列表窗"左上角贴光标"的旧定位）。坐标换算：钩子给的是物理像素，
// Wails 窗口 API 按 DIP 工作，经 ScreenManager 换算并以就近显示器工作区钳位，
// 保证圆盘在屏幕边缘/多显示器下不被裁掉。
func (s *QuickMenuService) showAt(trg mousetrap.Trigger) {
	a := application.Get()
	if a == nil || a.Screen == nil {
		return
	}

	// 弹窗按需就位：隐藏驻留的热复用（取消空闲销毁计时），已销毁/从未建则此刻重建。
	popup := s.acquirePopup()
	if popup == nil {
		return // 应用实例不可用（理论上不可达，防御）
	}

	physical := application.Point{X: int(trg.X), Y: int(trg.Y)}
	dip := a.Screen.PhysicalToDipPoint(physical)

	// 盘心对准光标：方形窗口左上角回退半盘（列表时代的"左上贴光标"对轮盘是错的）。
	w, h := popup.Size()
	x, y := dip.X-w/2, dip.Y-h/2
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
	s.clipPopup(popup)
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

// clipPopup 把弹窗按当前物理客户区重裁为整圆区域（ClipWindowEllipse）。窗口本体
// 已是真透明（BackgroundTypeTransparent），圆盘视觉边缘由页面抗锯齿绘制；区域裁剪
// 只承担命中测试——把四角从鼠标命中里剪掉让点击穿透到下层应用。裁剪圈半径 = 盘半径
// + 透明边距，落在投影淡出后的全透明区，GDI 硬边在视觉上不可见。
//
// 必须每次唤出重裁而非一劳永逸：SetWindowRgn 区域不随窗口改尺寸，而"DIP 固定 512"
// 的窗口跨到不同缩放比的显示器后物理像素必变（Wails 在 WM_DPICHANGED 里按建议矩形
// 直接改窗），一次性的圈会停在旧半径把圆盘歪着切掉一块——即"轮盘变形"。另挂
// WindowDPIChanged 事件兜底，覆盖"先裁后到 DPI 重排"的竞态。单枚 GDI 调用成本，
// 可安全重放；失败仅损失四角穿透体验，首报 Warn 后续降 Debug 不刷屏。
func (s *QuickMenuService) clipPopup(popup *application.WebviewWindow) {
	if err := windows.ClipWindowEllipse(uintptr(popup.NativeWindow())); err != nil {
		s.mu.Lock()
		first := !s.clipWarned
		s.clipWarned = true
		s.mu.Unlock()
		if first {
			slog.Warn("quickmenu: 圆盘裁剪失败（四角点击穿透降级，后续唤出仍重试）", "err", err)
		} else {
			slog.Debug("quickmenu: 圆盘裁剪失败", "err", err)
		}
	}
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

// GetStatus 返回快捷菜单运行态（模块页展示 + 二级轮盘开关回显）。
func (s *QuickMenuService) GetStatus() (Status, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Status{}, gateErr
	}
	defer release()

	hold, move := s.effectiveTrigger()
	return Status{
		TrapActive: s.trapActive(),
		HoldMs:     int(hold / time.Millisecond),
		MoveTol:    move,
		ItemCount:  len(s.wheelView()),
		TwoTier:    s.twoTierOn(),
	}, nil
}

// GetTwoTier 返回二级轮盘开关状态（模块页独立读取用）。
func (s *QuickMenuService) GetTwoTier() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()

	return s.twoTierOn(), nil
}

// SetTwoTier 保存二级轮盘开关：开启时分组扇区点击展开子盘，关闭时分组子条目
// 拍平进主盘。热生效——弹窗每次唤出都经 wheelView 重算，无需重启。
func (s *QuickMenuService) SetTwoTier(on bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if s.store == nil {
		return fmt.Errorf("配置存储不可用")
	}
	return s.store.SetQuickMenuTwoTier(on)
}

// twoTierOn 读取二级轮盘开关（store 缺失时保守按关闭处理）。
func (s *QuickMenuService) twoTierOn() bool {
	return s.store != nil && s.store.GetQuickMenuTwoTier()
}

// GetTriggerConfig 返回当前生效触发参数（钳制后，模块页表单初值）。
func (s *QuickMenuService) GetTriggerConfig() (holdMs, movePx int, err error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return 0, 0, gateErr
	}
	defer release()
	hold, move := s.effectiveTrigger()
	return int(hold / time.Millisecond), move, nil
}

// SetTriggerConfig 保存触发参数并热重启鼠标钩子（N5-C2）。入参先钳进合法域再落盘
// ——越界不报错回显钳后值（数值输入框防呆口径，与字号族一致）；store 不可用如实拒。
func (s *QuickMenuService) SetTriggerConfig(holdMs, movePx int) (int, int, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return 0, 0, gateErr
	}
	defer release()

	if s.store == nil {
		return 0, 0, fmt.Errorf("配置存储不可用")
	}
	hold, move := effectiveTrigger(holdMs, movePx) // 先钳制再落盘：盘上只存合法域值
	hMs := int(hold / time.Millisecond)
	if err := s.store.SetQuickMenuTrigger(hMs, move); err != nil {
		return 0, 0, err
	}
	if err := s.restartTrap(); err != nil {
		return hMs, move, err
	}
	return hMs, move, nil
}

// restartTrap 以当前生效参数热换鼠标钩子（未启动则零操作）。旧泵线程随
// Stop 关闭通道自然收束 consumeEvents；mousetrap 全进程单例约束下 Start
// 自带前置清理，重启失败如实报错（旧钩子此时已停，页面重试即可恢复）。
func (s *QuickMenuService) restartTrap() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	old := s.trap
	s.trap = nil
	s.mu.Unlock()

	_ = old.Stop()
	hold, move := s.effectiveTrigger()
	trap, err := mousetrap.Start(mousetrap.Config{MinHold: hold, MaxMove: int32(move)})
	if err != nil {
		slog.Warn("quickmenu: 触发参数热重启失败", "err", err)
		return err
	}
	s.mu.Lock()
	if !s.started { // stop 在途抢跑：刚起的钩子立即归还，不留孤儿线程
		s.mu.Unlock()
		_ = trap.Stop()
		return nil
	}
	s.trap = trap
	s.mu.Unlock()
	go s.consumeEvents(trap)
	slog.Info("quickmenu: 触发参数已热更新", "hold", hold, "moveTol", move)
	return nil
}

// wheelNode 轮盘展示结构节点：一条主盘扇区（叶子或分组 + 已过滤的启用子条目）。
type wheelNode struct {
	item settings.TrayMenuItem
	kids []settings.TrayMenuItem
}

// wheelView 计算轮盘当前展示结构：过滤启用条目，二级开关开启时保留 group 树形
// （空组不占位），关闭时把组内启用子条目直接拍平为主盘扇区。ListItems 与 Launch
// 共用本视图，Index 即展示序下标，天然一致。
func (s *QuickMenuService) wheelView() []wheelNode {
	var out []wheelNode
	for _, item := range s.disp.EnabledItems() {
		if item.Type == settings.TrayItemGroup {
			kids := enabledLeaves(item.Children)
			if !s.twoTierOn() {
				for _, k := range kids {
					out = append(out, wheelNode{item: k})
				}
				continue
			}
			if len(kids) == 0 {
				continue
			}
			out = append(out, wheelNode{item: item, kids: kids})
			continue
		}
		out = append(out, wheelNode{item: item})
	}
	return out
}

// enabledLeaves 过滤组内启用的叶子条目（防御性拒绝嵌套分组，配置层已校验）。
func enabledLeaves(items []settings.TrayMenuItem) []settings.TrayMenuItem {
	var out []settings.TrayMenuItem
	for _, ch := range items {
		if ch.Enabled && ch.Type != settings.TrayItemGroup {
			out = append(out, ch)
		}
	}
	return out
}

// ListItems 返回弹窗菜单条目树（复用托盘配置中启用的条目，展示序即索引序；
// 二级轮盘关闭时 group 已被拍平，树只有一层）。
func (s *QuickMenuService) ListItems() ([]MenuItem, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	view := s.wheelView()
	out := make([]MenuItem, 0, len(view))
	for i, node := range view {
		mi := s.menuItem(i, node.item)
		mi.Children = make([]MenuItem, 0, len(node.kids))
		for j, k := range node.kids {
			mi.Children = append(mi.Children, s.menuItem(j, k))
		}
		out = append(out, mi)
	}
	return out, nil
}

// menuItem 把配置条目解析为一个轮盘扇区视图模型（Index 为所在层展示序）。
func (s *QuickMenuService) menuItem(index int, item settings.TrayMenuItem) MenuItem {
	hint := item.Ref
	if item.Type == settings.TrayItemExe {
		hint = item.Path
	}
	return MenuItem{
		Index: index,
		Label: s.disp.Label(item),
		Type:  item.Type,
		Hint:  hint,
		Icon:  s.resolveIcon(item),
	}
}

// resolveIcon 为扇区解析前端图标名（AppIcon 注册表约定）：页面类复用导航注册的
// 模块图标（"i:" 前缀剥离），命令类用 terminal，程序类用 box，分组用 layers；
// 路由已停用查不到 nav 时回退 layout。前端对未登记图标名仍有最终回退。
func (s *QuickMenuService) resolveIcon(item settings.TrayMenuItem) string {
	switch item.Type {
	case settings.TrayItemRoute:
		if s.registry != nil {
			for _, nav := range s.registry.GetEnabledNavs() {
				if nav.Route == item.Ref {
					return strings.TrimPrefix(nav.Icon, "i:")
				}
			}
		}
		return "layout"
	case settings.TrayItemCommand:
		// 命令条目优先解析其归属模块的导航图标（ref 形如 "moduleId/commandId"，
		// 模块页路由约定 /ext/<moduleId>）——修机主反馈"轮盘命令扇区全是通用
		// 方块"：app: 前缀原样透传（前端 wheelIconOf 二轨认得），i: 剥前缀与
		// route 分支同口径；查不到导航仍回 terminal，渲染不断链。
		if s.registry != nil {
			mod := item.Ref
			if i := strings.IndexByte(mod, '/'); i > 0 {
				mod = mod[:i]
			}
			for _, nav := range s.registry.GetEnabledNavs() {
				if nav.Route == "/ext/"+mod {
					return strings.TrimPrefix(nav.Icon, "i:")
				}
			}
		}
		return "terminal"
	case settings.TrayItemGroup:
		return "layers"
	default:
		return "box"
	}
}

// Launch 按展示序路径派发条目：[i] 主盘第 i 个扇区；[i, j] 主盘分组 i 的第 j 个
// 子条目（二级轮盘关闭时后端已拍平，前端只会传一元路径，二元路径被拍平结构自然
// 拒绝）。与 ListItems 共用 wheelView，索引一致。先收起弹窗给即时反馈，派发进
// goroutine，失败统一走通知 Hub（与托盘失败反馈同构）。
func (s *QuickMenuService) Launch(path []int) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if len(path) == 0 {
		return fmt.Errorf("未指定菜单条目")
	}
	view := s.wheelView()
	i := path[0]
	if i < 0 || i >= len(view) {
		return fmt.Errorf("菜单条目不存在（索引 %d）", i)
	}
	node := view[i]
	if len(path) >= 2 {
		j := path[1]
		if len(node.kids) == 0 {
			return fmt.Errorf("该条目不是分组（索引 %d）", i)
		}
		if j < 0 || j >= len(node.kids) {
			return fmt.Errorf("分组子条目不存在（索引 %d）", j)
		}
		node = wheelNode{item: node.kids[j]}
	} else if len(node.kids) > 0 {
		return fmt.Errorf("分组条目请点击展开子盘，本身不可执行")
	}
	item := node.item

	s.dismiss() // 内部无门版：本调用已持门租约，不再二次入账
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
func (s *QuickMenuService) OpenSettings() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.navigateMain("/settings")
	s.dismiss()
	return nil
}

// Dismiss 收起弹窗（前端 Esc / 空背景点击调用的 RPC 导出版：接统一调用门），并武装空闲销毁。
func (s *QuickMenuService) Dismiss() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.dismiss()
	return nil
}

// dismiss 收起弹窗的内部无门版：供带门方法内部复用（避免同一调用链重复入账）。
// 窗体事件（Closing/LostFocus/点击外部）本就直接走 hidePopup，不经此入口。
func (s *QuickMenuService) dismiss() {
	s.hidePopup()
}

func (s *QuickMenuService) trapActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && s.trap != nil
}
