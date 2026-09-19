package app

import (
	"fmt"
	"sync"
	"testing"

	"hanxi/internal/hotkey"
)

// stubHotkeyBackend hotkey.Backend 测试桩：以内存集合模拟 OS 键位占用，
// 记账 Register/Unregister 调用次数供幂等断言。
type stubHotkeyBackend struct {
	mu        sync.Mutex
	keys      map[string]struct{}
	regCalls  int
	unregCall int
}

func newStubHotkeyBackend() *stubHotkeyBackend {
	return &stubHotkeyBackend{keys: make(map[string]struct{})}
}

func (s *stubHotkeyBackend) Register(accel string, _ func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.keys[accel]; dup {
		return fmt.Errorf("%s already registered", accel)
	}
	s.keys[accel] = struct{}{}
	s.regCalls++
	return nil
}

func (s *stubHotkeyBackend) Unregister(accel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[accel]; !ok {
		return fmt.Errorf("%s not registered", accel)
	}
	delete(s.keys, accel)
	s.unregCall++
	return nil
}

func (s *stubHotkeyBackend) IsRegistered(accel string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.keys[accel]
	return ok
}

func (s *stubHotkeyBackend) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.regCalls, s.unregCall
}

// TestSnipHotkeyGate 表驱动覆盖"模块启用 × 用户开关"双因子状态机：
// 停用→注销、启用且开关开→绑定、启用但开关关→不绑定、重复启停幂等不报错。
func TestSnipHotkeyGate(t *testing.T) {
	cases := []struct {
		name          string
		moduleEnabled bool
		userEnabled   bool
		wantBound     bool
	}{
		{"启用且开关开→绑定", true, true, true},
		{"启用但开关关→不绑定", true, false, false},
		{"停用（开关仍开）→注销释放键位", false, true, false},
		{"停用且开关关→不绑定", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend := newStubHotkeyBackend()
			hk := hotkey.NewRegistry(backend)
			gate := newSnipHotkeyGate(hk, func() (string, bool) {
				return "Ctrl+Alt+T", c.userEnabled
			}, func() {})

			gate.onLifecycle(ocrModuleID, c.moduleEnabled)
			if got := hk.Registered(snipHotkeyName); got != c.wantBound {
				t.Errorf("Registered()=%v, want %v", got, c.wantBound)
			}
			if !c.wantBound && backend.IsRegistered("Ctrl+Alt+T") {
				t.Errorf("期望态为不绑定，OS 键位不应被占用")
			}

			// 幂等：重复同态启停不报错、不再触碰底层（同键重绑 no-op、无槽解绑 no-op）
			reg0, unreg0 := backend.counts()
			gate.apply(c.moduleEnabled)
			gate.apply(c.moduleEnabled)
			if reg1, unreg1 := backend.counts(); reg1 != reg0 || unreg1 != unreg0 {
				t.Errorf("重复 apply 应幂等：底层调用从 (%d,%d) 变为 (%d,%d)", reg0, unreg0, reg1, unreg1)
			}
		})
	}
}

// TestSnipHotkeyGateLifecycleTransitions 启停往复与旁路事件：停用注销后重新
// 启用可恢复绑定；非 ocr 模块事件不动 snip 槽位。
func TestSnipHotkeyGateLifecycleTransitions(t *testing.T) {
	backend := newStubHotkeyBackend()
	hk := hotkey.NewRegistry(backend)
	userEnabled := true
	gate := newSnipHotkeyGate(hk, func() (string, bool) {
		return "Ctrl+Alt+T", userEnabled
	}, func() {})

	gate.onLifecycle(ocrModuleID, true)
	if !hk.Registered(snipHotkeyName) {
		t.Fatal("启用+开关开应绑定")
	}
	gate.onLifecycle(ocrModuleID, false)
	if hk.Registered(snipHotkeyName) {
		t.Fatal("停用应注销热键释放 OS 键位")
	}
	// 停用期间改键位也不应绑定
	gate.onLifecycle("msgboard", true) // 无关模块事件：no-op
	if hk.Registered(snipHotkeyName) {
		t.Fatal("无关模块事件不应重绑 snip 槽位")
	}
	gate.onLifecycle(ocrModuleID, true)
	if !hk.Registered(snipHotkeyName) {
		t.Fatal("重新启用且开关仍开应恢复绑定")
	}
	// 重新启用时用户开关已关 → 不绑定
	userEnabled = false
	gate.onLifecycle(ocrModuleID, false)
	gate.onLifecycle(ocrModuleID, true)
	if hk.Registered(snipHotkeyName) || backend.IsRegistered("Ctrl+Alt+T") {
		t.Fatal("用户开关已关，重新启用模块不应重绑")
	}
}
