package msgboard

import (
	"errors"
	"strings"
	"testing"

	"hanxi/internal/hotkey"
)

// 热键收编迁移回归（R1）：服务层挂上通用热键注册器（internal/hotkey）后，
// 改键原子回滚（冲突时旧键必须仍活、且从未被注销）、停用注销、随模块
// start/stop 注销与重启重绑、GetStatus 实况上报——这些正是收编前私有封装
// 的行为承诺，迁移后逐条钉死，防接缝债复发。

// stubHotkeyBackend hotkey.Backend 测试桩：登记簿模拟 OS 占用，regErr 可编程
// 注入冲突（与 hotkey 包内桩同形，此处验证的是服务层到槽位的整条链路）。
type stubHotkeyBackend struct {
	registered map[string]bool
	cbs        map[string]func()
	calls      []string
	regErr     func(accel string) error
}

func newStubHotkey() *stubHotkeyBackend {
	return &stubHotkeyBackend{registered: map[string]bool{}, cbs: map[string]func(){}}
}

func (st *stubHotkeyBackend) Register(accel string, cb func()) error {
	st.calls = append(st.calls, "register:"+accel)
	if st.regErr != nil {
		if err := st.regErr(accel); err != nil {
			return err
		}
	}
	st.registered[accel] = true
	st.cbs[accel] = cb
	return nil
}

func (st *stubHotkeyBackend) Unregister(accel string) error {
	st.calls = append(st.calls, "unregister:"+accel)
	if !st.registered[accel] {
		return errors.New(`global shortcut "` + accel + `" is not registered`)
	}
	delete(st.registered, accel)
	delete(st.cbs, accel)
	return nil
}

func (st *stubHotkeyBackend) IsRegistered(accel string) bool { return st.registered[accel] }

func (st *stubHotkeyBackend) lastCalls(n int) []string {
	if len(st.calls) < n {
		n = len(st.calls)
	}
	return st.calls[len(st.calls)-n:]
}

// newWiredService 装配"注册器已注入"形态的服务（模拟装配根 app.go 接线）。
func newWiredService(t *testing.T) (*MsgBoardService, *stubHotkeyBackend) {
	t.Helper()
	s := newTestService(t)
	st := newStubHotkey()
	s.setHotkeyRegistry(hotkey.NewRegistry(st))
	return s, st
}

// start 即绑默认热键；stop 注销（停用收口）；再次 start 重绑（重启重绑）。
// 中途模拟启动期 OS 回滚残账（记账在、系统没绑）：实况上报必须如实报 false。
func TestHotkeyStartStopRebind(t *testing.T) {
	s, st := newWiredService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if !st.registered[defaultHotkey] || !s.GetStatus().HotkeyActive {
		t.Fatalf("start 后默认热键应在位: %v", st.calls)
	}

	// OS 侧掉绑（等价启动期 pending 回滚）：自记不谎报，实况 false。
	delete(st.registered, defaultHotkey)
	if st2 := s.GetStatus(); st2.HotkeyActive {
		t.Fatal("系统未绑定时 HotkeyActive 不得谎报 true")
	}

	if err := s.stop(); err != nil {
		t.Fatal(err)
	}
	if len(st.registered) != 0 {
		t.Fatalf("stop 应注销热键，实存 %v", st.registered)
	}

	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if !st.registered[defaultHotkey] || !s.GetStatus().HotkeyActive {
		t.Fatal("重启后必须重新绑定")
	}
}

// 改键成功：先注册新键、成功后才注销旧键（顺序即原子语义），配置与实况同步。
func TestHotkeyRebindSuccess(t *testing.T) {
	s, st := newWiredService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	old := s.store.Get()
	if err := s.SetConfig(Config{Text: old.Text, FontSize: old.FontSize, Screen: old.Screen, Hotkey: "Ctrl+Alt+R"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.lastCalls(2), ","); got != "register:Ctrl+Alt+R,unregister:Ctrl+Alt+B" {
		t.Fatalf("换键调用序列失真（须先新后旧）: %v", st.calls)
	}
	if !st.registered["Ctrl+Alt+R"] || st.registered[defaultHotkey] {
		t.Fatalf("换键后系统占用表失真: %v", st.registered)
	}
	if got := s.GetStatus(); !got.HotkeyActive || got.Hotkey != "Ctrl+Alt+R" {
		t.Fatalf("状态回显失真: %+v", got)
	}
}

// 改键冲突（新键被其他软件抢注）：报错回前端，配置回滚旧值，
// 旧键全程未被注销、仍然活着——注册器"先注册新键成功才注销旧键"语义。
func TestHotkeyRebindConflictKeepsOldKeyAlive(t *testing.T) {
	s, st := newWiredService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	st.regErr = func(accel string) error {
		if accel == "Ctrl+Alt+J" {
			return errors.New(`the shortcut "Ctrl+Alt+J" is already registered (possibly by another application)`)
		}
		return nil
	}
	old := s.store.Get()
	err := s.SetConfig(Config{Text: "会议中", FontSize: old.FontSize, Screen: old.Screen, Hotkey: "Ctrl+Alt+J"})
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("冲突应给改键指引: %v", err)
	}
	if got := s.store.Get().Hotkey; got != defaultHotkey {
		t.Fatalf("冲突后配置热键应回滚 %q，实为 %q", defaultHotkey, got)
	}
	if !st.registered[defaultHotkey] {
		t.Fatalf("冲突后旧键必须仍在系统上活着: %v", st.registered)
	}
	for _, c := range st.calls {
		if c == "unregister:"+defaultHotkey {
			t.Fatalf("冲突路径绝不允许先注销旧键（会短暂丢键）: %v", st.calls)
		}
	}
	if got := s.GetStatus(); !got.HotkeyActive {
		t.Fatal("旧键仍在位，状态不得报 inactive")
	}
	// 正文等非热键字段照常保留（回滚只回热键字段）。
	if got := s.store.Get().Text; got != "会议中" {
		t.Fatalf("非热键字段应照常落盘，实为 %q", got)
	}
}

// 停用热键（键位清空）：解绑幂等、配置留空、状态如实 inactive。
func TestHotkeyDisableUnbinds(t *testing.T) {
	s, st := newWiredService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	old := s.store.Get()
	if err := s.SetConfig(Config{Text: old.Text, FontSize: old.FontSize, Screen: old.Screen, Hotkey: ""}); err != nil {
		t.Fatalf("停用热键不应报错: %v", err)
	}
	if len(st.registered) != 0 {
		t.Fatalf("停用后系统不应留有绑定: %v", st.registered)
	}
	if got := s.GetStatus(); got.HotkeyActive || got.Hotkey != "" {
		t.Fatalf("停用状态失真: %+v", got)
	}
	// 停用态再 start/stop 一圈，不得凭空注册。
	if err := s.stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if len(st.registered) != 0 {
		t.Fatalf("停用配置重启不得复活热键: %v", st.registered)
	}
}

// 回调接线：注册进系统的热键回调就是挂/撤牌入口（无头触发走服务层守卫，
// 不 panic、不改动挂牌状态）。
func TestHotkeyCallbackWiredToToggle(t *testing.T) {
	s, st := newWiredService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	cb := st.cbs[defaultHotkey]
	if cb == nil {
		t.Fatal("注册回调缺失")
	}
	cb() // 无头：Toggle→Show 报"需应用运行"，仅记日志
	if got := s.GetStatus(); got.Shown {
		t.Fatal("无头触发回调不应进入已挂出态")
	}
}
