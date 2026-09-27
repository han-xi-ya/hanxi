package clipboard

import (
	"errors"
	"sync"
	"testing"

	"hanxi/internal/hotkey"
)

// fakeHotkeyBackend hotkey.Backend 桩件：记录在绑键与回调，冲突键可注入。
type fakeHotkeyBackend struct {
	mu           sync.Mutex
	registered   map[string]bool
	handlers     map[string]func()
	conflictKeys map[string]bool
}

func newFakeHotkeyBackend() *fakeHotkeyBackend {
	return &fakeHotkeyBackend{
		registered:   map[string]bool{},
		handlers:     map[string]func(){},
		conflictKeys: map[string]bool{},
	}
}

func (f *fakeHotkeyBackend) Register(accel string, cb func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conflictKeys[accel] {
		return errors.New("already registered (possibly by another application)")
	}
	f.registered[accel] = true
	f.handlers[accel] = cb
	return nil
}

func (f *fakeHotkeyBackend) Unregister(accel string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.registered, accel)
	delete(f.handlers, accel)
	return nil
}

func (f *fakeHotkeyBackend) IsRegistered(accel string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registered[accel]
}

// TestClipHotkeyLifecycle start 绑默认 Ctrl+Alt+V（槽 clipboard/overlay），
// stop 摘键；注册器未接线时 start/stop 照常成功（降级只记日志）。
func TestClipHotkeyLifecycle(t *testing.T) {
	h := newSvcHarness(t) // 无注册器启动：已覆盖"未接线不炸"
	defer h.stop()
	if err := h.svc.stop(); err != nil {
		t.Fatal(err)
	}

	fb := newFakeHotkeyBackend()
	reg := hotkey.NewRegistry(fb)
	h.svc.setHotkeyRegistry(reg)
	if err := h.svc.start(); err != nil {
		t.Fatalf("start with registry: %v", err)
	}
	if !reg.Registered(overlayHotkeySlot) {
		t.Fatal("start 未按槽位绑定热键")
	}
	if !fb.IsRegistered("Ctrl+Alt+V") {
		t.Fatalf("默认键位应为 Ctrl+Alt+V，实况: %v", fb.registered)
	}

	// 回调派发（模拟 Wails goroutine）：无头态 toggleOverlay 返回可读错误，
	// 只 warn 不 panic——onOverlayHotkey 吞错语义验证
	fb.mu.Lock()
	cb := fb.handlers["Ctrl+Alt+V"]
	fb.mu.Unlock()
	if cb == nil {
		t.Fatal("注册器未收到回调")
	}
	cb()

	if err := h.svc.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if reg.Registered(overlayHotkeySlot) || fb.IsRegistered("Ctrl+Alt+V") {
		t.Fatal("stop 未摘热键（留按键黑洞）")
	}
}

// TestClipHotkeyConflictDegrades 抢键冲突：start 不报错不 panic（降级只记
// 日志、监听照常起），槽位如实报未在位（口径同 memo/msgboard）。
func TestClipHotkeyConflictDegrades(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop() // stop 幂等，先行停机后 defer 再走一遍无害

	if err := h.svc.stop(); err != nil {
		t.Fatal(err)
	}
	fb := newFakeHotkeyBackend()
	fb.conflictKeys[defaultOverlayHotkey] = true
	reg := hotkey.NewRegistry(fb)
	h.svc.setHotkeyRegistry(reg)

	if err := h.svc.start(); err != nil {
		t.Fatalf("热键冲突不得让 start 失败: %v", err)
	}
	if reg.Registered(overlayHotkeySlot) {
		t.Fatal("冲突降级后槽位应如实报未在位")
	}
	// 采集面不受热键拖累：捕获照常入库
	h.svc.handleCapture(textCap("热键被占也要记"))
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("热键降级不应牵连采集: %d", n)
	}
}
