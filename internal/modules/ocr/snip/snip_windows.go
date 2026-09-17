//go:build windows

package snip

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard     = user32.NewProc("OpenClipboard")
	procCloseClipboard    = user32.NewProc("CloseClipboard")
	procEmptyClipboard    = user32.NewProc("EmptyClipboard")
	procGetClipboardData  = user32.NewProc("GetClipboardData")
	procSetClipboardData  = user32.NewProc("SetClipboardData")
	procIsFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterFormat    = user32.NewProc("RegisterClipboardFormatW")
	procGlobalSize        = kernel32.NewProc("GlobalSize")
	procGlobalLock        = kernel32.NewProc("GlobalLock")
	procGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
	procGlobalFree        = kernel32.NewProc("GlobalFree")
	procGlobalAlloc       = kernel32.NewProc("GlobalAlloc")
	procMoveMemory        = kernel32.NewProc("RtlMoveMemory")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
)

const (
	cfUnicodeText = 0x000D
	cfDIB         = 0x0008
	gmemMoveable  = 0x0002
)

// pngFormatID 解析剪贴板私有 "PNG" 格式 ID（Win11 截屏工具通常同时提供，
// 比 CF_DIB 更省一次解码）。RegisterClipboardFormatW 幂等：已注册即回原 ID。
var pngFormatID = sync.OnceValue(func() uint32 {
	p, _ := syscall.UTF16PtrFromString("PNG")
	r, _, _ := procRegisterFormat.Call(uintptr(unsafe.Pointer(p)))
	if r != 0 && r != 0xFFFF {
		return uint32(r)
	}
	return 0
})()

// system 单例实现 Snipper（进程级无状态，剪贴板调用自带重试串行化）。
type system struct{}

// New 返回当前平台的截屏/剪贴板原语实现。
func New() Snipper { return system{} }

func (system) InvokeOverlay() error {
	// 同 wechat/open_windows.go 的协议拉起先例：rundll32 无控制台，天然不闪窗。
	// ms-screenclip: 唤起 Win11「屏幕截图」（与 Win+Shift+S 同一覆盖层）。
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", "ms-screenclip:").Start()
}

// openClipboard 带重试打开：覆盖层/资源管理器等会在完成瞬间持有剪贴板锁。
func openClipboard() error {
	for i := 0; i < 6; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("剪贴板被其他程序占用，稍后重试")
}

// readFormat 取指定格式字节（调用方负责已 OpenClipboard）。
func readFormat(format uint32) ([]byte, bool, error) {
	if r, _, _ := procIsFormatAvailable.Call(uintptr(format)); r == 0 {
		return nil, false, nil
	}
	h, _, _ := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil, false, nil // 有格式但取不到句柄：按未命中处理，继续轮询
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 || size > 256<<20 { // 256MB 上限防异常尺寸拖爆内存
		return nil, false, fmt.Errorf("剪贴板数据大小异常(%d B)", size)
	}
	p, _, errno := procGlobalLock.Call(h)
	if p == 0 {
		return nil, false, fmt.Errorf("锁定剪贴板内存失败: %w", errno)
	}
	// 数据复制走 RtlMoveMemory：系统内存地址 p 全程保持 uintptr，
	// 不做 uintptr→unsafe.Pointer 转换（vet/unsafeptr 对 syscall 返回值的
	// 反向转换无白名单，此形态是 syscall 内存复制的标准洁净写法）。
	data := make([]byte, size)
	syscall.Syscall(procMoveMemory.Addr(), 3, uintptr(unsafe.Pointer(&data[0])), p, size)
	// 解锁失败惯例忽略（对象在用时的伪错；CloseClipboard 后系统自管）
	procGlobalUnlock.Call(h)
	return data, true, nil
}

func (system) SnapshotText() (string, bool) {
	if err := openClipboard(); err != nil {
		return "", false
	}
	defer procCloseClipboard.Call()
	data, ok, err := readFormat(cfUnicodeText)
	if !ok || err != nil {
		return "", false
	}
	// UTF-16 → string，去尾零
	var u []uint16
	for i := 0; i+1 < len(data); i += 2 {
		c := binary16(data[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u), len(u) > 0
}

func binary16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

func (system) Empty() error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("清空剪贴板失败: %w", err)
	}
	return nil
}

func (system) WriteText(text string) error {
	// 截屏覆盖层可能尚未退出仍持锁：与读取同款重试。
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("文本编码失败: %w", err)
	}
	byteLen := uintptr(len(u) * 2)
	for i := 0; i < 6; i++ {
		if werr := writeTextOnce(u, byteLen); werr == nil {
			return nil
		} else if i == 5 {
			return werr
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func writeTextOnce(u []uint16, byteLen uintptr) error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("清空剪贴板失败: %w", err)
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, byteLen)
	if h == 0 {
		return fmt.Errorf("分配剪贴板内存失败: %w", err)
	}
	p, _, errno := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("锁定剪贴板内存失败: %w", errno)
	}
	// RtlMoveMemory 写方向：理由同 readFormat（p 保持 uintptr 不转换）。
	syscall.Syscall(procMoveMemory.Addr(), 3, p, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u))*2)
	procGlobalUnlock.Call(h)
	// SetClipboardData 成功后内存归系统；失败才由我方释放。
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("写入剪贴板失败: %w", err)
	}
	return nil
}

func (system) GrabImage() ([]byte, bool, error) {
	if err := openClipboard(); err != nil {
		return nil, false, err
	}
	defer procCloseClipboard.Call()
	// 优先私有 PNG 格式（零转码）
	if pngFormatID != 0 {
		if data, found, err := readFormat(pngFormatID); found || err != nil {
			return data, found, err
		}
	}
	dib, found, err := readFormat(cfDIB)
	if !found || err != nil {
		return nil, found, err
	}
	pngBytes, err := DecodeDIBToPNG(dib)
	if err != nil {
		return nil, false, fmt.Errorf("剪贴板图像格式不支持: %w", err)
	}
	return pngBytes, true, nil
}

func (system) CursorPos() (int, int, error) {
	type point struct{ X, Y int32 }
	var pt point
	if r, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, fmt.Errorf("获取光标位置失败: %w", err)
	}
	return int(pt.X), int(pt.Y), nil
}
