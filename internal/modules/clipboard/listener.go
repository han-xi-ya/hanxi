package clipboard

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

// 系统剪贴板监听（契约 §5）：message-only 窗口 + AddClipboardFormatListener 订阅
// WM_CLIPBOARDUPDATE，在独立 goroutine（锁线程）上跑消息泵——本包全用
// golang.org/x/sys 裸 syscall 直调 user32/kernel32/shell32，不引任何新依赖。
//
// 读取优先序 CF_UNICODETEXT → CF_DIB → CF_HDROP（同屏多格式并存时文本优先，
// 与入库去重的语义字节口径一致）。回环抑制：本进程 Set 回填后系统照样广播
// WM_CLIPBOARDUPDATE，GetClipboardOwner 的窗口属主进程==自己 → 静默跳过。
//
// 生命周期：startClipboardListener 建窗挂监听并等首帧就绪（失败如实报错，由
// 服务层降级记日志，绝不 panic）；stop 走 WM_CLOSE→DestroyWindow→WM_DESTROY→
// PostQuitMessage 正规通路，泵线程随 goroutine 退出释放。

const (
	wmClipboardUpdate = 0x031D
	wmClose           = 0x0010
	wmDestroy         = 0x0002

	cfUnicodeText = 0x000D
	cfDIB         = 0x0008
	cfHDROP       = 0x000F

	gmemMoveable = 0x0002

	processQueryLimitedInformation = 0x1000

	// HWND_MESSAGE：message-only 窗口父句柄伪值 = (HWND)-3，即 0xFFFFFFFFFFFFFFFD。
	// 历史教训（2026-09-27 实测）：曾误写 ^uintptr(0)（-1 是 HWND_BROADCAST），
	// CreateWindowEx 直接 1408 Invalid window handle，监听静默停摆、历史全空。
	// 判据自证：^uintptr(2) == 0-3 常量折叠同值，误再犯则常量断言编译不过。
	hwndMessage = ^uintptr(2) // = (HWND)-3 = HWND_MESSAGE

	// OpenClipboard 争用重试：1 次正试 + 至多 5 次重试（契约/任务口径"重试≤5"），
	// 覆盖层/资源管理器瞬持剪贴板锁的窗口期（snip 同款节奏）。
	clipOpenAttempts = 6
	clipOpenRetryGap = 50 * time.Millisecond
)

var (
	user32Clip   = xw.NewLazySystemDLL("user32.dll")
	kernel32Clip = xw.NewLazySystemDLL("kernel32.dll")
	shell32Clip  = xw.NewLazySystemDLL("shell32.dll")

	procAddClipListener    = user32Clip.NewProc("AddClipboardFormatListener")
	procRemoveClipListener = user32Clip.NewProc("RemoveClipboardFormatListener")
	procGetClipboardOwner  = user32Clip.NewProc("GetClipboardOwner")
	procOpenClipboard      = user32Clip.NewProc("OpenClipboard")
	procCloseClipboard     = user32Clip.NewProc("CloseClipboard")
	procEmptyClipboard     = user32Clip.NewProc("EmptyClipboard")
	procIsFormatAvailable  = user32Clip.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData   = user32Clip.NewProc("GetClipboardData")
	procSetClipboardData   = user32Clip.NewProc("SetClipboardData")
	procGetMessage         = user32Clip.NewProc("GetMessageW")
	procTranslateMessage   = user32Clip.NewProc("TranslateMessage")
	procDispatchMessage    = user32Clip.NewProc("DispatchMessageW")
	procDefWindowProc      = user32Clip.NewProc("DefWindowProcW")
	procRegisterClassEx    = user32Clip.NewProc("RegisterClassExW")
	procCreateWindowEx     = user32Clip.NewProc("CreateWindowExW")
	procDestroyWindow      = user32Clip.NewProc("DestroyWindow")
	procPostMessage        = user32Clip.NewProc("PostMessageW")
	procPostQuitMessage    = user32Clip.NewProc("PostQuitMessage")
	procGetFgWindow        = user32Clip.NewProc("GetForegroundWindow")
	procGetWindowThreadPID = user32Clip.NewProc("GetWindowThreadProcessId")
	procGetWindowTextLen   = user32Clip.NewProc("GetWindowTextLengthW")
	procGetWindowText      = user32Clip.NewProc("GetWindowTextW")
	procOpenProcess        = kernel32Clip.NewProc("OpenProcess")

	procGlobalSize   = kernel32Clip.NewProc("GlobalSize")
	procGlobalLock   = kernel32Clip.NewProc("GlobalLock")
	procGlobalUnlock = kernel32Clip.NewProc("GlobalUnlock")
	procGlobalAlloc  = kernel32Clip.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32Clip.NewProc("GlobalFree")
	procMoveMemory   = kernel32Clip.NewProc("RtlMoveMemory")
	procQueryImgName = kernel32Clip.NewProc("QueryFullProcessImageNameW")

	procDragQueryFile = shell32Clip.NewProc("DragQueryFileW")
)

// msg/dispatch 消息泵所需的最小 Win32 结构（x/sys 未导出 Msg/WNDCLASSEX，
// 布局与 Win32 MSG/WNDCLASSEXA 逐字段对齐，amd64 无 packing 歧义）。
type winMsg struct {
	Hwnd    xw.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type wndClassEx struct {
	Size          uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     xw.Handle
	HIcon         xw.Handle
	HCursor       xw.Handle
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       xw.Handle
}

const listenerClassName = "hanxiClipboardListener"

// activeListener 进程级单线（一个服务一个监听）：wndproc 是包级函数指针，
// 只能经全局定位回实例。start 建窗后写入、run 退出时清空，重叠窗口理论
// 不存在（服务生命周期单例，stop 等泵线程退出后才可能再 start）。
var activeListener atomic.Pointer[clipboardListener]

var (
	listenerClassOnce sync.Once
	listenerClassErr  error
)

// capture 一次剪贴板捕获的原始载荷（监听层→服务层的交接物；kind 之外的
// 入库决策——排除表/敏感判定/钳制——全部在服务与存储层做，监听不做业务判断）。
type capture struct {
	kind      Kind
	text      string
	dib       []byte
	files     []string
	sourceApp string
	sourceExe string
	at        time.Time
}

// clipboardListener message-only 监听窗宿主。
type clipboardListener struct {
	onCapture func(capture)

	hwnd  xw.Handle     // run 线程写入（ready 交接后只读）
	ready chan error    // 建窗+挂监听结果一次性交接
	done  chan struct{} // 消息泵退出（stop 等待用）
}

// startClipboardListener 起监听 goroutine 并等就绪；失败已自清理，返回错误
// 由调用方降级（历史采集暂停，其余功能可用），绝不 panic。
func startClipboardListener(onCapture func(capture)) (*clipboardListener, error) {
	if onCapture == nil {
		return nil, fmt.Errorf("剪贴板监听缺少回调")
	}
	l := &clipboardListener{
		onCapture: onCapture,
		ready:     make(chan error, 1),
		done:      make(chan struct{}),
	}
	go l.run()
	if err := <-l.ready; err != nil {
		<-l.done // 失败路径泵线程已自退，收口再返回
		return nil, err
	}
	return l, nil
}

// stop 关窗收泵：PostMessage(WM_CLOSE) 在泵线程外安全投递，等消息线程退出。
func (l *clipboardListener) stop() {
	if l == nil || l.hwnd == 0 {
		return
	}
	procPostMessage.Call(uintptr(l.hwnd), wmClose, 0, 0)
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		slog.Warn("clipboard: 监听线程 5s 未退出（消息泵被外部卡死？），放弃等待")
	}
}

// run 监听线程主体：锁线程 → 注册类 → message-only 建窗 → 挂格式监听 → 消息泵。
func (l *clipboardListener) run() {
	defer close(l.done)
	defer runtime.UnlockOSThread()
	runtime.LockOSThread()

	fail := func(err error) {
		l.ready <- err
	}

	if err := registerListenerClass(); err != nil {
		fail(fmt.Errorf("注册剪贴板监听窗口类失败: %w", err))
		return
	}
	className, err := syscall.UTF16PtrFromString(listenerClassName)
	if err != nil {
		fail(err)
		return
	}
	title, err := syscall.UTF16PtrFromString("hanxi-clipboard-listener")
	if err != nil {
		fail(err)
		return
	}
	hwnd, _, err := procCreateWindowEx.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		fail(fmt.Errorf("创建剪贴板监听窗口失败: %w", err))
		return
	}
	l.hwnd = xw.Handle(hwnd)
	if r, _, e := procAddClipListener.Call(hwnd); r == 0 {
		procDestroyWindow.Call(hwnd)
		fail(fmt.Errorf("挂接剪贴板格式监听失败: %w", e))
		return
	}

	activeListener.Store(l)
	l.ready <- nil

	var msg winMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 { // WM_QUIT：stop 通路（WM_CLOSE→DestroyWindow→PostQuitMessage）
			break
		}
		if int(r) == -1 { // 泵错误（句柄/权限异常）：记日志退出，上层按未监听降级处理
			slog.Warn("clipboard: 监听消息泵 GetMessage 失败，采集线程退出")
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	procRemoveClipListener.Call(uintptr(l.hwnd)) // DestroyWindow 已隐式摘除，兜底幂等
	if cur := activeListener.Load(); cur == l {
		activeListener.Store(nil)
	}
}

// registerListenerClass 窗口类进程级注册一次（失败缓存错误如实复用）。
func registerListenerClass() error {
	listenerClassOnce.Do(func() {
		className, err := syscall.UTF16PtrFromString(listenerClassName)
		if err != nil {
			listenerClassErr = err
			return
		}
		wc := wndClassEx{
			Size:          uint32(unsafe.Sizeof(wndClassEx{})),
			LpfnWndProc:   syscall.NewCallback(listenerWndProc),
			LpszClassName: className,
		}
		r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 && err != syscall.Errno(1418) { // 1418=ERROR_CLASS_ALREADY_EXISTS：类名照旧可用
			listenerClassErr = fmt.Errorf("RegisterClassExW: %w", err)
			return
		}
	})
	return listenerClassErr
}

// listenerWndProc 包级窗口过程（经 activeListener 定位实例；签名匹配 WNDPROC）。
func listenerWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmClipboardUpdate:
		if l := activeListener.Load(); l != nil {
			l.handleUpdate()
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// handleUpdate WM_CLIPBOARDUPDATE 派发（监听线程内同步执行）：回环抑制 →
// 带重试开板 → 按优先序取一种格式 → 附前台窗口来源 → 交回调入库。
func (l *clipboardListener) handleUpdate() {
	if clipboardOwnedBySelf() {
		return // 自写回环（Set 回填）：不入库
	}
	if err := openClipboardRetry(); err != nil {
		slog.Warn("clipboard: 打开剪贴板失败（他程序瞬持锁），本次更新跳过", "err", err)
		return
	}
	defer procCloseClipboard.Call()

	c := capture{at: time.Now()}
	c.sourceApp, c.sourceExe = foregroundSource()

	if raw, ok := clipFormatBytes(cfUnicodeText); ok && len(raw) >= 2 {
		if text := utf16BytesToString(raw); text != "" && strings.TrimSpace(text) != "" {
			c.kind, c.text = KindText, text
			l.onCapture(c)
			return
		}
		// 有 CF_UNICODETEXT 但全空白：记了纯属噪音，视同无内容
		return
	}
	if dib, ok := clipFormatBytes(cfDIB); ok && len(dib) > 0 {
		c.kind, c.dib = KindImage, dib
		l.onCapture(c)
		return
	}
	if files, ok := readClipFiles(); ok && len(files) > 0 {
		c.kind, c.files = KindFile, files
		l.onCapture(c)
	}
}

// clipboardOwnedBySelf 剪贴板属主窗口是否属本进程（GetClipboardOwner →
// GetWindowThreadProcessId → pid 比对；契约 §5 回环抑制口径）。
func clipboardOwnedBySelf() bool {
	owner, _, _ := procGetClipboardOwner.Call()
	if owner == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadPID.Call(owner, uintptr(unsafe.Pointer(&pid)))
	return pid == xw.GetCurrentProcessId()
}

// openClipboardRetry 1 正试 + ≤5 重试（50ms 间隔），全败才报错。
func openClipboardRetry() error {
	var lastErr error
	for range clipOpenAttempts {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		lastErr = err
		time.Sleep(clipOpenRetryGap)
	}
	return fmt.Errorf("剪贴板被其他程序占用（重试 %d 次仍失败）: %w", clipOpenAttempts-1, lastErr)
}

// clipFormatBytes 取指定格式全局内存副本（调用方须已开板）。全局句柄按
// GlobalSize→GlobalLock→RtlMoveMemory→Unlock 标准链读取（snip 同谱，
// 不解引用 uintptr 指针，规避 vet 质疑）。
func clipFormatBytes(format uint32) ([]byte, bool) {
	if r, _, _ := procIsFormatAvailable.Call(uintptr(format)); r == 0 {
		return nil, false
	}
	h, _, _ := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil, false
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return nil, false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil, false
	}
	buf := make([]byte, size)
	procMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, size)
	procGlobalUnlock.Call(h) // 对象在用时 Unlock 伪错，惯例忽略
	return buf, true
}

// utf16BytesToString UTF-16LE 字节 → Go 字符串（截断到首个 NUL）。
// raw 为自己 make 的 []byte，取址转换安全。
func utf16BytesToString(raw []byte) string {
	if len(raw) < 2 {
		return ""
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(&raw[0])), len(raw)/2)
	return syscall.UTF16ToString(units)
}

// readClipFiles CF_HDROP 文件列表（DragQueryFileW 枚举；HDROP 无需 GlobalLock）。
func readClipFiles() ([]string, bool) {
	if r, _, _ := procIsFormatAvailable.Call(cfHDROP); r == 0 {
		return nil, false
	}
	h, _, _ := procGetClipboardData.Call(cfHDROP)
	if h == 0 {
		return nil, false
	}
	count, _, _ := procDragQueryFile.Call(h, ^uintptr(0), 0, 0) // 0xFFFFFFFF=取文件数
	if count == 0 || count == ^uintptr(0) {
		return nil, false
	}
	files := make([]string, 0, count)
	buf := make([]uint16, 32768) // 预留超长路径余量
	for i := range int(count) {
		n, _, _ := procDragQueryFile.Call(h, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 {
			files = append(files, syscall.UTF16ToString(buf[:n]))
		}
	}
	return files, len(files) > 0
}

// foregroundSource 前台窗口标题 + 小写进程名（复制来源，无头/取不到时为空串，
// 调用侧对空值全容忍）。
func foregroundSource() (title, exeLower string) {
	fg, _, _ := procGetFgWindow.Call()
	if fg == 0 {
		return "", ""
	}
	title = windowText(fg)
	var pid uint32
	procGetWindowThreadPID.Call(fg, uintptr(unsafe.Pointer(&pid)))
	return title, processExeName(pid)
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLen.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func processExeName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, _, err := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		slog.Debug("clipboard: 打开来源进程失败（来源进程名留空）", "pid", pid, "err", err)
		return ""
	}
	defer xw.CloseHandle(xw.Handle(h))
	buf := make([]uint16, 512)
	size := uint32(len(buf))
	r, _, err := procQueryImgName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		slog.Debug("clipboard: 查询来源进程映像名失败", "pid", pid, "err", err)
		return ""
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size])))
}

// setSystemText 文本回填系统剪贴板（服务 Set 的默认写手）：开板重试 →
// Empty → GlobalAlloc+MoveMemory（所有权随 SetClipboardData 成功移交系统，
// 失败自 Free——snip WriteText 同款纪律）。
func setSystemText(text string) error {
	if err := openClipboardRetry(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("清空系统剪贴板失败: %w", err)
	}
	units, uerr := xw.UTF16FromString(text) // 尾含 NUL（syscall.StringToUTF16 已弃用，走 x/sys 形态）
	if uerr != nil {
		return fmt.Errorf("文本含无法表达的 UTF-16 序列: %w", uerr)
	}
	byteLen := uintptr(len(units) * 2)
	h, _, err := procGlobalAlloc.Call(gmemMoveable, byteLen)
	if h == 0 {
		return fmt.Errorf("分配剪贴板内存失败: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("锁定剪贴板内存失败: %w", err)
	}
	procMoveMemory.Call(p, uintptr(unsafe.Pointer(&units[0])), byteLen)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h) // 失败才由我方释放
		return fmt.Errorf("写入系统剪贴板失败: %w", err)
	}
	return nil
}
