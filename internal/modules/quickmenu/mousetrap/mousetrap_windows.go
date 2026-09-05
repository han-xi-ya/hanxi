//go:build windows

package mousetrap

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 常量（x/sys/windows 未收录鼠标钩子相关定义，按本地约定就近声明）。
const (
	whMouseLL = 14 // WH_MOUSE_LL

	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmMButtonDown = 0x0207
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmQuit        = 0x0012
	wmApp         = 0x8000

	// 回调 → 消息泵的请求（PostThreadMessage wParam 载荷）。
	reqRightClick = 1 // 回放一组右键 按下+抬起（短按恢复原生上下文菜单）
	reqRightDown  = 2 // 只回放按下（右键拖拽让位，后续移动/抬起原样放行）
	reqThreshold  = 3 // Go 定时器判定按住已达阈值（见 startGestureTimer）

	llmhfInjected = 0x00000001 // MSLLHOOKSTRUCT.flags：事件源自 SendInput
	mouseInput    = 0          // INPUT.type == INPUT_MOUSE

	mouseeventfRightDo = 0x0008 // MOUSEEVENTF_RIGHTDOWN
	mouseeventfRightUp = 0x0010 // MOUSEEVENTF_RIGHTUP

	// replayMagic 盖在我们 SendInput 回放的 dwExtraInfo 上：本钩子据此精确放行
	// 自家回放事件，杜绝重放循环。比"逢 injected 标志即放行"更强——外部自动化
	// 注入的右键同样进入长按判定（与真实按键同权），而自家事件绝不自我吞入。
	replayMagic = uintptr(0x484A_5250) // "HRP"
)

// Win11 任务栏系窗口类名：系统自有 UI，长按手势在这些区域整体旁路、绝不吞事件
// （真机实证，见 TROUBLESHOOTING #29）。
//
// 判定刻意用"FindWindow + 矩形相交"而非 WindowFromPoint 命中测试：实测 Win11
// 的 XAML 任务栏整带上 WindowFromPoint 一律返回 0（即便 DPI 感知正确），命中
// 测试在这条链路上根本不可靠；矩形是 GetWindowRect 的物理像素，与钩子 pt 同系。
var taskbarWindowClasses = [...]string{
	"Shell_TrayWnd",
	"Shell_SecondaryTrayWnd",
	"TopLevelWindowForOverflowXamlIsland", // 托盘溢出（隐藏图标）区
}

// rc 对应 RECT。
type rc struct{ l, t, r, b int32 }

var (
	modUser32      = syscall.NewLazyDLL("user32.dll")
	procSetHook    = modUser32.NewProc("SetWindowsHookExW")
	procCallNext   = modUser32.NewProc("CallNextHookEx")
	procUnhook     = modUser32.NewProc("UnhookWindowsHookEx")
	procGetMessage = modUser32.NewProc("GetMessageW")
	procTranslate  = modUser32.NewProc("TranslateMessage")
	procDispatch   = modUser32.NewProc("DispatchMessageW")
	procPostThread = modUser32.NewProc("PostThreadMessageW")
	procSendInput  = modUser32.NewProc("SendInput")

	procFindWindowW   = modUser32.NewProc("FindWindowW")
	procGetWindowRect = modUser32.NewProc("GetWindowRect")
	procGetCursorPos  = modUser32.NewProc("GetCursorPos")
)

// 诊断一次性哨兵：日志一律在消息泵线程输出——钩子回调内写文件可能超过
// LowLevelHooksTimeout 被系统静默摘钩，回调里只置原子标志。
var (
	replayWarned       atomic.Bool // SendInput 返回不足数（失败）
	injectedSeenLogged atomic.Bool // 注入右键事件确实回到了钩子链

)

// msllHookStruct 对应 MSLLHOOKSTRUCT（amd64/arm64 布局；仅读 pt 与 extraInfo）。
type msllHookStruct struct {
	X, Y      int32
	mouseData uint32
	flags     uint32
	time_     uint32
	_         uint32 // x64 对齐填充
	extraInfo uintptr
}

// point 对应 POINT（GetCursorPos / WindowFromPoint 参数）。
type point struct{ X, Y int32 }

// msg 对应 MSG；message/wParam 用于在消息泵里识别回放与阈值请求。
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX, ptY int32
	lPrivate uint32
}

// cinput 对应 x64 INPUT（仅取 MOUSEINPUT 联合成员；cbSize 由 unsafe.Sizeof 推导）。
type cinput struct {
	typ uint32
	_   uint32
	mi  struct {
		dx, dy    int32
		mouseData uint32
		flags     uint32
		time      uint32
		_         uint32 // x64 对齐填充
		extraInfo uintptr
	}
}

// trapState 钩子回调私有状态：只在安装钩子的那条消息循环线程上读写，无需加锁
// （AfterFunc 回调不触碰本结构，只向本线程 PostThreadMessage）。
type trapState struct {
	trap        *Trap
	cfg         Config
	armed       bool        // 右键按下已吞、待判长按/短按
	suppressed  bool        // 阈值已触发弹窗：本次手势的抬起必须吞净
	revealed    bool        // 拖拽让位已发生：按下已补回放，后续事件放行
	timer       *time.Timer // 阈值一次性定时器（Reset 复用，Stop/Reset 均在本线程）
	sawInjected bool        // 回调观察到注入右键（由泵线程择机记日志）
	t0          time.Time
	sx, sy      int32
}

// endGesture 终结当前待判手势：停表并复位判定标志（各终止路径共用）。
func (st *trapState) endGesture() {
	if st.timer != nil {
		st.timer.Stop()
	}
	st.armed = false
	st.suppressed = false
	st.revealed = false
}

// activeTrap 当前活动识别器；回调经原子指针读取，Stop 收尾后置 nil。
var activeTrap atomic.Pointer[trapState]

// Trap 一个运行中的右键长按识别器。
type Trap struct {
	ch     chan Trigger
	clicks chan ButtonEvent
	tid    uint32
	done   chan struct{}
}

// Supported 报告当前平台是否支持全局鼠标长按钩子。
func Supported() bool { return true }

// Start 安装低级鼠标钩子并开始识别。触发与观察事件均从缓冲通道非阻塞投递
// （消费端未就绪时丢弃，绝不背压——卡死系统输入链远比丢一次触发糟糕）。
func Start(cfg Config) (*Trap, error) {
	stopActive()

	t := &Trap{
		ch:     make(chan Trigger, 1),
		clicks: make(chan ButtonEvent, 8),
		done:   make(chan struct{}),
	}
	st := &trapState{trap: t, cfg: cfg.normalized()}

	errCh := make(chan error, 1)
	go func() {
		defer close(t.done)
		// 低级钩子回调要求在安装线程上泵消息，必须锁线程。
		runtime.LockOSThread()
		activeTrap.Store(st)

		hhook, _, err := procSetHook.Call(
			uintptr(whMouseLL),
			syscall.NewCallback(hookProc),
			0, // WH_MOUSE_LL 不注入，hMod 允许为 0
			0, // 0 = 全系统线程（低级钩子唯一允许值）
		)
		if hhook == 0 {
			activeTrap.Store(nil)
			if err == nil {
				err = errors.New("SetWindowsHookExW 返回空句柄")
			}
			errCh <- fmt.Errorf("安装全局鼠标钩子失败: %w", err)
			return
		}
		t.tid = windows.GetCurrentThreadId()
		errCh <- nil

		var m msg
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			// GetMessageW 返回 0=WM_QUIT 退出、-1=错误，均终止循环（截断 int32 判断）。
			if int32(uint32(r)) <= 0 {
				break
			}
			// 线程消息（hwnd=0）的 WM_APP：回放/阈值请求一律在泵线程处理——
			// SendInput、阈值判定、日志都不进钩子回调，杜绝重入与超时摘钩。
			// 刻意不用 CreateThreadTimer：内核线程定时器到期经用户 APC 执行回调，
			// 真机实测会把进程直接打崩（见 TROUBLESHOOTING #29），Go AfterFunc +
			// PostThreadMessage 语义等价且零内核 APC。
			if m.hwnd == 0 && m.message == wmApp {
				switch m.wParam {
				case reqRightClick:
					sendRight(mouseeventfRightDo, mouseeventfRightUp)
				case reqRightDown:
					sendRight(mouseeventfRightDo)
				case reqThreshold:
					onThreshold(st)
				}
				if st.sawInjected && !injectedSeenLogged.Swap(true) {
					slog.Debug("mousetrap: 注入的右键事件已回到钩子链（回放通路正常）")
				}
				continue
			}
			procTranslate.Call(uintptr(unsafe.Pointer(&m)))
			procDispatch.Call(uintptr(unsafe.Pointer(&m)))
		}
		st.endGesture()
		procUnhook.Call(hhook)
		activeTrap.Store(nil)
		close(t.ch)
		close(t.clicks)
	}()

	if err := <-errCh; err != nil {
		<-t.done
		return nil, err
	}
	return t, nil
}

// C 返回长按触发通道；Stop 完成后关闭。
func (t *Trap) C() <-chan Trigger { return t.ch }

// Buttons 返回真实鼠标键按下观察通道（含右键），供消费侧做"点击外部收起"兜底；
// Stop 完成后关闭。
func (t *Trap) Buttons() <-chan ButtonEvent { return t.clicks }

// Stop 请求钩子线程退出（PostThreadMessage WM_QUIT），等待消息循环收尾并摘钩。
func (t *Trap) Stop() error {
	if t == nil {
		return nil
	}
	if t.tid != 0 {
		// 投递 WM_QUIT 终结消息循环；投递失败（线程已消亡）时也走 done 超时兜底，调用方不悬挂。
		procPostThread.Call(uintptr(t.tid), uintptr(wmQuit), 0, 0)
	}
	select {
	case <-t.done:
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("鼠标钩子线程未在 3s 内退出")
	}
}

// stopActive 停掉既有识别器（全进程单例约束下的 Start 前置清理）。
func stopActive() {
	if st := activeTrap.Load(); st != nil && st.trap != nil {
		_ = st.trap.Stop()
	}
}

// startGestureTimer 为本手势复位阈值定时器：到期后 AfterFunc 回调（Go 定时器线程）
// 仅向泵线程投递 reqThreshold，判定与投递全部回到泵线程完成。
func startGestureTimer(st *trapState) {
	if st.timer == nil {
		st.timer = time.AfterFunc(st.cfg.MinHold, func() {
			postRequest(st, reqThreshold)
		})
		return
	}
	st.timer.Reset(st.cfg.MinHold)
}

// onThreshold 泵线程上响应阈值请求：按住已达阈值且手势仍在手上——
// 立即弹窗（松手前触发），并把本手势标记为 suppressed，抬手时吞净不回放。
func onThreshold(st *trapState) {
	if !st.armed || st.revealed || st.suppressed {
		return
	}
	st.suppressed = true
	var p point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); r != 0 {
		deliver(st, Trigger{X: p.X, Y: p.Y, Hold: time.Since(st.t0)})
	}
}

// deliver 非阻塞投递触发事件。
func deliver(st *trapState, trg Trigger) {
	select {
	case st.trap.ch <- trg:
	default: // 消费端积压：丢弃本次，绝不阻塞输入链
	}
}

// hookInfo 从 lParam（uintptr 形态的 MSLLHOOKSTRUCT 指针）还原结构体指针。
// 经取局部变量地址再解引用的惯用法完成转换，满足 vet 的 unsafeptr 检查
// （uintptr→Pointer 直转会被判定为可能误用）。
func hookInfo(lParam uintptr) *msllHookStruct {
	return *(**msllHookStruct)(unsafe.Pointer(&lParam))
}

// isTaskbarSurface 判定物理坐标是否落在任务栏系（含托盘溢出区）窗口矩形内。
// 每手势仅右键按下时调用一次；FindWindow+GetWindowRect 为纯查询，无消息往返。
func isTaskbarSurface(x, y int32) bool {
	for _, cls := range taskbarWindowClasses {
		wide, err := syscall.UTF16PtrFromString(cls)
		if err != nil {
			continue
		}
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(wide)), 0)
		if hwnd == 0 {
			continue
		}
		var r rc
		if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
			continue
		}
		if r.l <= x && x < r.r && r.t <= y && y < r.b {
			return true
		}
	}
	return false
}

// sendRight 在消息泵线程注入右键事件序列（extraInfo 盖自家魔术值，回调据此放行，
// 不会二次进入长按判定形成重放循环）。
func sendRight(flags ...uint32) {
	if len(flags) == 0 {
		return
	}
	inputs := make([]cinput, len(flags))
	for i, f := range flags {
		inputs[i].typ = mouseInput
		inputs[i].mi.flags = f
		inputs[i].mi.extraInfo = replayMagic
	}
	// 参数序 = SendInput(cInputs, pInputs, cbSize)。
	r, _, errno := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(inputs[0])),
	)
	if r != uintptr(len(flags)) {
		if !replayWarned.Swap(true) {
			slog.Warn("mousetrap: SendInput 回放失败，该次右键将丢失（首次告警）",
				"want", len(flags), "sent", r, "err", errno)
		}
	}
}

// hookProc WH_MOUSE_LL 回调：识别策略见包注释。回调内零阻塞、零日志，回放与阈值
// 判定一律 PostThreadMessage 转交消息泵（任务栏命中测试除外，每手势仅一次）。
func hookProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		if st := activeTrap.Load(); st != nil {
			info := hookInfo(lParam)
			ours := info.extraInfo == replayMagic
			if info.flags&llmhfInjected != 0 && (wParam == wmRButtonDown || wParam == wmRButtonUp) {
				st.sawInjected = true // 只置标志，日志由泵线程择机输出
			}
			switch wParam {
			case wmRButtonDown:
				if !ours {
					if isTaskbarSurface(info.X, info.Y) {
						break // 任务栏/托盘：完全不拦截，交还原生行为
					}
					notifyClick(st, info)
					st.armed = true
					st.revealed = false
					st.suppressed = false
					st.t0 = time.Now()
					st.sx, st.sy = info.X, info.Y
					startGestureTimer(st)
					return 1 // 吞按下：系统与前台应用的按键状态保持干净
				}
			case wmRButtonUp:
				if ours {
					break
				}

				// 阈值已弹窗：吞净本手势的抬起并全量复位（应用全程未见右键）。
				// 后续任何新手势都从 DOWN 重新武装，不依赖本分支保留状态。
				if st.suppressed {
					st.endGesture()
					return 1
				}
				if st.armed {
					st.armed = false
					if st.timer != nil {
						st.timer.Stop()
					}
					switch {
					case st.revealed:
						// 拖拽让位中：放行抬起，应用自己收尾拖拽。
					case time.Since(st.t0) >= st.cfg.MinHold:
						// 阈值判定迟到（少见）：抬手才达阈值，按长按处理。
						deliver(st, Trigger{X: info.X, Y: info.Y, Hold: time.Since(st.t0)})
						return 1
					default:
						postRequest(st, reqRightClick) // 普通短按：回放完整右键
						return 1
					}
				}
			case wmMouseMove:
				if st.armed && !st.revealed && !st.suppressed &&
					(abs32(info.X-st.sx) > st.cfg.MaxMove || abs32(info.Y-st.sy) > st.cfg.MaxMove) {
					st.revealed = true
					if st.timer != nil {
						st.timer.Stop()
					}
					postRequest(st, reqRightDown) // 拖拽成形：补回按下，交还应用
				}
			case wmLButtonDown, wmMButtonDown:
				notifyClick(st, info)
				// 阈值前被左/中键打断：作废长按判定，补回这一下右键。
				// 阈值后（suppressed）不打断——本手势的抬起仍将吞净（交还给
				// WM_RBUTTONUP 的 suppressed 分支），否则原生菜单会盖在弹窗上。
				if st.armed && !st.revealed && !st.suppressed {
					st.armed = false
					if st.timer != nil {
						st.timer.Stop()
					}
					postRequest(st, reqRightClick)
				}
			}
		}
	}
	ret, _, _ := procCallNext.Call(uintptr(nCode), wParam, lParam)
	return ret
}

// notifyClick 非阻塞广播一次真实按键按下（观察通道满则丢弃，不影响输入链）。
func notifyClick(st *trapState, info *msllHookStruct) {
	select {
	case st.trap.clicks <- ButtonEvent{X: info.X, Y: info.Y}:
	default:
	}
}

// postRequest 向钩子消息泵投递请求。回调与泵同线程；AfterFunc 在 Go 定时器线程
// 调用 PostThreadMessage 本身线程安全。泵线程未就绪（tid=0）时静默丢弃。
func postRequest(st *trapState, req uintptr) {
	if tid := atomic.LoadUint32(&st.trap.tid); tid != 0 {
		procPostThread.Call(uintptr(tid), wmApp, req, 0)
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
