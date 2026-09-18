package msgboard

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/hotkey"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// 挂牌窗口常量：与轮盘/悬浮卡同谱的 frameless 真透明窗（BackgroundTypeTransparent
// + RGBA(0,0,0,0)），牌体半透深色底由 MsgBoardPopup 页面自绘——窗口本体透明
// 杜绝任何窗底白边（透明窗露底坑见 docs/TROUBLESHOOTING.md #50）。
const (
	boardWindowName = "msgboard-board"
	boardWindowURL  = "/#msgboard"
	keepAwakeHolder = "msgboard" // 防休眠聚合器持有人登记名（平台层引用计数）
	eventChanged    = "msgboard:changed"
)

// MsgBoardService 桌面留言板：一键在目标显示器全屏挂出离岗告示牌。
//
// 三通道唤起：托盘/轮盘共用 extapi.TrayCommandsProvider 注册的 toggle 命令
// （条目配置与分发走 internal/launcher 现成通道）；全局热键收编入
// internal/hotkey 通用注册器槽位 msgboard/toggle（接线与语义见 hotkey.go）。
//
// 生命周期纪律：挂牌窗口按需创建、撤牌即真销毁（对齐 #53：注销 WindowClosing
// 拦截 hook 后 Close 走 Wails 内部销毁路径，WebView2 内存归还，同名窗口可重建，
// 不得白边/残影）；不养常驻隐藏窗。显隐判定走服务层状态机（shown），窗口 API
// （Show/Close/Fullscreen 均为主线程 InvokeSync）一律在 s.mu 之外调用，防锁反转。
type MsgBoardService struct {
	plat  platform.Platform
	store *msgBoardStore

	mu          sync.Mutex
	cond        *sync.Cond
	started     bool
	stopping    bool // stop 已接管：拒绝新 Show，等待在途操作收口
	opInFlight  bool // 串行挂牌操作租约；等待者由 cond 唤醒，不再静默丢弃 Dismiss
	generation  uint64
	shown       bool
	board       *application.WebviewWindow
	offClosing  func()           // WindowClosing 拦截 hook 的注销闭包（销毁前摘除，#53 通路）
	hk          *hotkey.Registry // 全仓通用热键注册器（装配根注入，槽位记账归注册器）
	keepAwakeOn bool             // 防休眠诉求是否在账（状态页如实回显）
}

// NewMsgBoardService 装配服务单例：构造仅读盘建 store，不碰窗口与热键
// （懒建于 OnInit/start，与 quickmenu 同策略）。
func NewMsgBoardService(plat platform.Platform, paths *settings.Paths) *MsgBoardService {
	return newMsgBoardService(plat, newMsgBoardStore(paths.StateDir()))
}

func newMsgBoardService(plat platform.Platform, store *msgBoardStore) *MsgBoardService {
	s := &MsgBoardService{plat: plat, store: store}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// ---------- 生命周期（由 module.go 的 OnInit/OnDestroy 驱动） ----------

// start 注册全局热键（开机常驻监听型能力，与 quickmenu 钩子同族）。热键注册
// 失败不致命：只降级为托盘/轮盘唤起，状态页如实显示"热键未在位"。
func (s *MsgBoardService) start() error {
	s.mu.Lock()
	for s.stopping {
		s.cond.Wait()
	}
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.stopping = false
	s.started = true
	s.generation++
	s.mu.Unlock()

	if key := s.store.Get().Hotkey; key != "" {
		if err := s.applyHotkey(key); err != nil {
			slog.Warn("msgboard: 全局热键注册失败（已降级为托盘/轮盘唤起）", "hotkey", key, "err", err)
		}
	}
	slog.Info("msgboard: 留言板服务已启动")
	return nil
}

// stop 摘热键、接管并等待在途 Show、撤牌与释放防休眠。模块停用/应用退出
// 返回时保证最终无窗、shown=false、KeepAwake/热键均已释放。
func (s *MsgBoardService) stop() error {
	s.mu.Lock()
	if !s.started && !s.stopping {
		s.mu.Unlock()
		return nil
	}
	if s.stopping {
		for s.stopping {
			s.cond.Wait()
		}
		s.mu.Unlock()
		return nil
	}
	s.started = false
	s.stopping = true
	s.generation++ // 使已取得 Show 租约但尚未提交的结果作废
	for s.opInFlight {
		s.cond.Wait()
	}
	s.opInFlight = true // stop 独占最后一次 Dismiss，阻止并发操作插队
	s.mu.Unlock()

	// 不持服务锁调用热键/Wails/平台能力，避免与其内部主线程锁形成反向等待。
	if r := s.hotkeyRegistry(); r != nil {
		if err := r.Unbind(hotkeySlot); err != nil {
			slog.Warn("msgboard: 全局热键注销失败", "err", err)
		}
	}
	s.dismissOwned()

	s.mu.Lock()
	s.opInFlight = false
	s.stopping = false
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}

// ---------- 挂牌 / 撤牌（三通道共同汇聚点） ----------

// Toggle 挂出↔撤牌一键互切（托盘/轮盘命令与热键回调的入口）。
func (s *MsgBoardService) Toggle() error {
	s.mu.Lock()
	for s.opInFlight && !s.stopping {
		s.cond.Wait()
	}
	shown := s.shown
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		return nil
	}
	if shown {
		s.Dismiss()
		return nil
	}
	return s.Show()
}

// Show 在全屏透明窗挂出留言牌：定位目标显示器 → 真全屏 → 置前抢焦点（Esc
// 直达）→ 登记防休眠。操作严格串行；stop 一旦开始，新的或在途 Show 都不能
// 在停用完成后提交窗口状态。
func (s *MsgBoardService) Show() error {
	generation, ok := s.beginShow()
	if !ok {
		return nil
	}
	defer s.endOperation()

	a := application.Get()
	if a == nil || a.Window == nil {
		return fmt.Errorf("留言板需要在应用运行后唤出，请稍后重试")
	}
	cfg := s.store.Get()

	s.mu.Lock()
	if s.shown {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	scr := findScreen(a.Screen, cfg.Screen)
	if scr == nil {
		return fmt.Errorf("未检测到可用显示器，无法挂牌")
	}

	board := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             boardWindowName,
		Title:            "留言牌",
		X:                scr.Bounds.X,
		Y:                scr.Bounds.Y,
		Width:            scr.Bounds.Width,
		Height:           scr.Bounds.Height,
		Hidden:           true, // 建窗即隐藏，摆位完成后一次性露出，防半帧闪
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundType:   application.BackgroundTypeTransparent,            // 真透明：牌体观感全部由页面绘制（#50）
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true}, // 不进任务栏/Alt+Tab
		URL:              boardWindowURL,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	// Alt+F4 拦截为撤牌；销毁动作 go 异步，避免在 WM_CLOSE 派发栈内重入 Close。
	offClosing := board.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		ev.Cancel()
		go s.Dismiss()
	})

	board.Show()
	// Fullscreen 由 Windows 实现按窗口所在显示器取 MonitorFromWindow +
	// SetWindowPos 铺满整个物理监视器（含任务栏区域、跨缩放比精确），
	// 比 DIP 尺寸手摆更可靠——这是"全屏挂牌"的语义本体。
	board.Fullscreen()
	// Wails 的 Focus 是裸 SetForegroundWindow，主窗藏托盘时被前台锁拒绝——
	// 借 quickmenu 同款输入特权强制置前，否则页面 Esc 收不到。
	if err := windows.SetForegroundForce(uintptr(board.NativeWindow())); err != nil {
		slog.Debug("msgboard: 留言牌强制置前失败（依赖托盘/轮盘撤牌）", "err", err)
	}

	// stop 可能在建窗期间宣告停用。提交前复核 generation；失效结果必须由
	// 当前 Show 自行真销毁，不能把清理责任留给已经在等待的 stop。
	s.mu.Lock()
	stale := s.stopping || !s.started || s.generation != generation
	if !stale {
		s.board, s.offClosing, s.shown = board, offClosing, true
	}
	s.mu.Unlock()
	if stale {
		offClosing()
		board.Close()
		return nil
	}

	s.acquireKeepAwake()
	s.emitChanged()
	slog.Info("msgboard: 已挂出留言牌", "screen", scr.Name, "primary", scr.IsPrimary)
	return nil
}

// Dismiss 撤牌并真销毁窗口（摘 WindowClosing hook 后 Close 走 Wails 内部销毁
// 路径，#53），同时释放防休眠诉求。若 Show 在途则等待并接管其终态；stop
// 接管期间普通 Dismiss 无需争抢。
func (s *MsgBoardService) Dismiss() {
	if !s.beginDismiss() {
		return
	}
	defer s.endOperation()
	s.dismissOwned()
}

func (s *MsgBoardService) dismissOwned() {
	s.mu.Lock()
	board, off := s.board, s.offClosing
	hadState := board != nil || s.shown || s.keepAwakeOn
	s.board, s.offClosing, s.shown = nil, nil, false
	s.mu.Unlock()

	// 即使本地 keepAwakeOn 为 false 也幂等 Release：Acquire 成功与状态标记之间
	// 若遇停用接管，仍由聚合器持有人账本兜底收口。
	s.releaseKeepAwake()
	if off != nil {
		off()
	}
	if board != nil {
		board.Close()
	}
	if hadState {
		s.emitChanged()
		slog.Info("msgboard: 留言牌已撤下（窗口已真销毁，WebView2 视图内存释放）")
	}
}

// beginShow/beginDismiss/endOperation 构成串行状态机。等待只发生在 cond 上，
// Wails、热键与 KeepAwake 调用均在 s.mu 外执行，避免锁反转。
func (s *MsgBoardService) beginShow() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.opInFlight && !s.stopping {
		s.cond.Wait()
	}
	if s.stopping || !s.started {
		return 0, false
	}
	s.opInFlight = true
	return s.generation, true
}

func (s *MsgBoardService) beginDismiss() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.opInFlight && !s.stopping {
		s.cond.Wait()
	}
	if s.stopping {
		return false
	}
	s.opInFlight = true
	return true
}

func (s *MsgBoardService) endOperation() {
	s.mu.Lock()
	s.opInFlight = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

// ---------- 防休眠（平台层引用计数聚合器的消费方） ----------

func (s *MsgBoardService) acquireKeepAwake() {
	if s.plat == nil {
		return
	}
	if err := s.plat.KeepAwake().Acquire(keepAwakeHolder, platform.KeepAwakeSystem|platform.KeepAwakeDisplay); err != nil {
		slog.Warn("msgboard: 防休眠登记失败（仍照常挂牌，屏可能按时熄灭）", "err", err)
		s.setKeepAwakeOn(false)
		return
	}

	// Acquire 不持服务锁；stop 可能已在此期间开始。若生命周期已失效，立即
	// 归还刚取得的租约，绝不在停用完成后留下 KeepAwake 持有人。
	s.mu.Lock()
	valid := s.started && !s.stopping && s.shown
	if valid {
		s.keepAwakeOn = true
	}
	s.mu.Unlock()
	if !valid {
		if err := s.plat.KeepAwake().Release(keepAwakeHolder); err != nil {
			slog.Warn("msgboard: 停用接管时防休眠释放失败", "err", err)
		}
	}
}

func (s *MsgBoardService) releaseKeepAwake() {
	if s.plat == nil {
		return
	}
	if err := s.plat.KeepAwake().Release(keepAwakeHolder); err != nil {
		slog.Warn("msgboard: 防休眠释放失败", "err", err)
	}
	s.setKeepAwakeOn(false)
}

func (s *MsgBoardService) setKeepAwakeOn(on bool) {
	s.mu.Lock()
	s.keepAwakeOn = on
	s.mu.Unlock()
}

// ---------- 前端绑定 API（模块页 + 挂牌弹窗共用） ----------

// GetStatus 返回运行态（模块页状态区回显）。热键在位与否问注册器槽位实况
// （Registry.Registered 底层即 manager.IsRegistered）而非自记状态：开机期
// Register 只入 pending、OS 拒绑发生在 Run 之后（错误走 Wails 错误通道不回流
// 本模块），自记标志会谎报，以系统实存为准。
func (s *MsgBoardService) GetStatus() Status {
	cfg := s.store.Get()
	s.mu.Lock()
	shown, awake := s.shown, s.keepAwakeOn
	s.mu.Unlock()
	r := s.hotkeyRegistry()
	active := r != nil && r.Registered(hotkeySlot)
	return Status{
		Shown:        shown,
		Hotkey:       cfg.Hotkey,
		HotkeyActive: active,
		KeepAwake:    awake,
	}
}

// GetConfig 返回偏好快照（模块页表单初值）。
func (s *MsgBoardService) GetConfig() Config { return s.store.Get() }

// SetConfig 保存偏好：正文/字号即时热更（弹窗拉新）；热键改判失败自动回滚
// 旧键并报错回前端；换屏则拆牌重挂（真销毁重建，与手动撤挂同路径）。
func (s *MsgBoardService) SetConfig(cfg Config) error {
	old := s.store.Get()
	next, err := s.store.Set(cfg)
	if err != nil {
		return err
	}
	if next.Hotkey != old.Hotkey {
		if herr := s.applyHotkey(next.Hotkey); herr != nil {
			// 新键不可用：注册器保旧绑定原样在位（先注册新键成功才注销旧键），
			// 配置热键字段回滚为旧值，错误上抛由页面红字提示改键。
			if _, rerr := s.store.Set(Config{Text: next.Text, FontSize: next.FontSize, Screen: next.Screen, Hotkey: old.Hotkey}); rerr != nil {
				slog.Warn("msgboard: 热键回滚落盘失败", "err", rerr)
			}
			return fmt.Errorf("热键 %q 注册失败（可能已被其它程序占用；留空表示停用热键）：%v", next.Hotkey, herr)
		}
	}
	s.emitChanged()
	if old.Screen != next.Screen {
		s.mu.Lock()
		shown := s.shown
		generation := s.generation
		active := s.started && !s.stopping
		s.mu.Unlock()
		if shown && active {
			go func() {
				s.Dismiss()
				s.mu.Lock()
				stillActive := s.started && !s.stopping && s.generation == generation
				s.mu.Unlock()
				if !stillActive {
					return
				}
				if err := s.Show(); err != nil {
					slog.Warn("msgboard: 换屏重挂失败", "err", err)
				}
			}()
		}
	}
	return nil
}

// ListPresets 返回内置预设文案模板（模块页一键填词候选）。
func (s *MsgBoardService) ListPresets() []string {
	out := make([]string, len(Presets))
	copy(out, Presets)
	return out
}

// ListScreens 返回挂牌可选显示器清单；应用未运行时返回空表（前端隐藏选择器）。
func (s *MsgBoardService) ListScreens() []ScreenInfo {
	a := application.Get()
	if a == nil || a.Screen == nil {
		return []ScreenInfo{}
	}
	screens := a.Screen.GetAll()
	out := make([]ScreenInfo, 0, len(screens))
	for _, scr := range screens {
		if scr == nil {
			continue
		}
		out = append(out, ScreenInfo{
			Device:    scr.Name,
			Width:     scr.PhysicalBounds.Width,
			Height:    scr.PhysicalBounds.Height,
			IsPrimary: scr.IsPrimary,
		})
	}
	return out
}

// GetBoardContent 挂牌弹窗拉取的正文（自定义为空回落第一条预设，永不空白）。
func (s *MsgBoardService) GetBoardContent() BoardContent {
	cfg := s.store.Get()
	return BoardContent{Text: effectiveText(cfg), FontSize: cfg.FontSize}
}

// ---------- 内部小件 ----------

// findScreen 按设备名取显示器；查不到（拔屏/改名）回退主屏，主屏异常再退
// 枚举首项——挂牌永远要有落点，绝不因配置里的旧设备名而拒绝服务。
func findScreen(sm *application.ScreenManager, device string) *application.Screen {
	if sm == nil {
		return nil
	}
	screens := sm.GetAll()
	if device != "" {
		for _, scr := range screens {
			if scr != nil && scr.Name == device {
				return scr
			}
		}
	}
	if p := sm.GetPrimary(); p != nil {
		return p
	}
	for _, scr := range screens {
		if scr != nil {
			return scr
		}
	}
	return nil
}

func (s *MsgBoardService) emitChanged() {
	if a := application.Get(); a != nil && a.Event != nil {
		a.Event.Emit(eventChanged) // 无载荷 Void 事件（注册见 app.go RegisterEvents）
	}
}
