package msgboard

import (
	"errors"
	"fmt"
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
	if !st.registered[defaultHotkey] || !statusOf(t, s).HotkeyActive {
		t.Fatalf("start 后默认热键应在位: %v", st.calls)
	}

	// OS 侧掉绑（等价启动期 pending 回滚）：自记不谎报，实况 false。
	delete(st.registered, defaultHotkey)
	if st2 := statusOf(t, s); st2.HotkeyActive {
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
	if !st.registered[defaultHotkey] || !statusOf(t, s).HotkeyActive {
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
	if got := statusOf(t, s); !got.HotkeyActive || got.Hotkey != "Ctrl+Alt+R" {
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
	if got := statusOf(t, s); !got.HotkeyActive {
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
	if got := statusOf(t, s); got.HotkeyActive || got.Hotkey != "" {
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
	cb() // 无头：toggle→show 报"需应用运行"，仅记日志
	if got := statusOf(t, s); got.Shown {
		t.Fatal("无头触发回调不应进入已挂出态")
	}
}

// statusOf 测试小件：经 RPC 导出版取状态（测试期 gate 未注入即放行）。
func statusOf(t *testing.T, s *MsgBoardService) Status {
	t.Helper()
	st, err := s.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	return st
}

// 一致性审查 A1 回归：改键冲突回滚路径不得吞掉 msgboard:changed 广播——
// 同一次保存里非热键字段（正文/字号）已如实落盘，牌面窗只认事件拉新，
// 漏播一次它就永远顶着旧文案示人（模块页尚有出错回读兜底，牌面没有）。
func TestSetConfigHotkeyConflictStillEmitsChanged(t *testing.T) {
	s, st := newWiredService(t)
	var events []string
	s.publish = func(name string) { events = append(events, name) }
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
	err := s.SetConfig(Config{Text: "改稿后的新文案", FontSize: old.FontSize, Screen: old.Screen, Hotkey: "Ctrl+Alt+J"})
	if err == nil {
		t.Fatal("改键冲突应报错回前端")
	}
	if got := s.store.Get().Hotkey; got != defaultHotkey {
		t.Fatalf("热键应回滚旧值，实为 %q", got)
	}
	if c, _ := s.GetBoardContent(); c.Text != "改稿后的新文案" {
		t.Fatalf("非热键字段应照常生效，实为 %q", c.Text)
	}
	n := 0
	for _, e := range events {
		if e == eventChanged {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("冲突路径也必须广播拉新（牌面窗唯一刷新源），events=%v", events)
	}
}

// uniqueHotkeyBackend 模拟 GlobalShortcutManager 的进程内规范键独占语义
// （canonical 小写判重、重复注册 "error and preserve"）——跨槽位共存实测用，
// 区别于上方 stubHotkeyBackend 的单槽位视角。
type uniqueHotkeyBackend struct {
	bindings map[string]func()
	calls    []string
}

func (u *uniqueHotkeyBackend) Register(accel string, cb func()) error {
	key := strings.ToLower(accel)
	if _, ok := u.bindings[key]; ok {
		return fmt.Errorf("global shortcut %q is already registered", accel)
	}
	u.bindings[key] = cb
	u.calls = append(u.calls, "register:"+accel)
	return nil
}

func (u *uniqueHotkeyBackend) Unregister(accel string) error {
	key := strings.ToLower(accel)
	if u.bindings[key] == nil {
		return fmt.Errorf("global shortcut %q is not registered", accel)
	}
	delete(u.bindings, key)
	u.calls = append(u.calls, "unregister:"+accel)
	return nil
}

func (u *uniqueHotkeyBackend) IsRegistered(accel string) bool {
	return u.bindings[strings.ToLower(accel)] != nil
}

// 跨槽位共存实证（65aa380 memo 速记新热键 Ctrl+Alt+N 与 msgboard/toggle 同接
// 一张 Registry）：两槽位记账互相独立——异槽抢键只伤抢的一方（中文报错、自己的
// 旧绑定原样活着），绝不注销他槽键位；回调各归各槽；本模块改键撞他槽在位键时
// 配置回滚、双方系统绑定均无损。
func TestHotkeyCrossSlotCoexistence(t *testing.T) {
	s := newTestService(t)
	ub := &uniqueHotkeyBackend{bindings: map[string]func(){}}
	r := hotkey.NewRegistry(ub)
	s.setHotkeyRegistry(r)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	memoFired := false
	if err := r.Bind("memo/quicksheet", "Ctrl+Alt+N", true, func() { memoFired = true }); err != nil {
		t.Fatalf("memo 槽位独立绑定应成功：%v", err)
	}
	if !ub.IsRegistered(defaultHotkey) || !ub.IsRegistered("Ctrl+Alt+N") {
		t.Fatalf("两槽位应同时在位：%v", ub.calls)
	}
	if cb := ub.bindings[strings.ToLower("Ctrl+Alt+B")]; cb == nil {
		t.Fatal("msgboard 键位回调缺失")
	}
	cbMsg := ub.bindings[strings.ToLower(defaultHotkey)]
	cbMsg() // 触发 msgboard 回调（无头 toggle 只报"需应用运行"，不伤他槽）
	if memoFired {
		t.Fatal("msgboard 槽位回调不得串门触发 memo 卡片")
	}

	// 异槽抢键：memo 试图改挂 msgboard 在位的 Ctrl+Alt+B——只许 memo 失败保旧。
	if err := r.Bind("memo/quicksheet", defaultHotkey, true, func() { memoFired = true }); err == nil {
		t.Fatal("抢他槽在位键应报错")
	}
	if !ub.IsRegistered("Ctrl+Alt+N") || ub.bindings[strings.ToLower("Ctrl+Alt+N")] == nil {
		t.Fatal("memo 抢键失败后旧绑定必须原样活着")
	}
	for _, c := range ub.calls {
		if c == "unregister:"+defaultHotkey || c == "unregister:Ctrl+Alt+N" {
			t.Fatalf("抢键失败路径绝不允许注销任何在位键：%v", ub.calls)
		}
	}

	// 反向：msgboard 经 SetConfig 改键撞 memo 在位的 Ctrl+Alt+N——配置回滚、
	// 旧键仍活、memo 分毫未动、状态如实。
	if err := s.SetConfig(Config{Text: "x", FontSize: 64, Screen: "", Hotkey: "Ctrl+Alt+N"}); err == nil {
		t.Fatal("改键撞他槽在位键应报错")
	}
	if got := s.store.Get().Hotkey; got != defaultHotkey {
		t.Fatalf("msgboard 热键应回滚旧值，实为 %q", got)
	}
	if !ub.IsRegistered(defaultHotkey) || !r.Registered(hotkeySlot) {
		t.Fatalf("msgboard 旧键必须无损（系统+槽账双层在位）：%v", ub.calls)
	}
	if !ub.IsRegistered("Ctrl+Alt+N") {
		t.Fatal("msgboard 改键失败不得波及 memo 槽位绑定")
	}
	if got := statusOf(t, s); !got.HotkeyActive {
		t.Fatal("旧键在位，状态不得谎报 inactive")
	}
}
