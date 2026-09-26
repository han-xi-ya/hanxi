package memo

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
)

// 速记热键链路测试（N16 B 批）：槽位绑定随 start/stop 收口、改键"先注册新键
// 成功才注销旧键"的原子序、冲突回滚（配置不动、旧键全程活着）、停用幂等、
// 注册器未接线的纯配置退化——语义钉死对齐 msgboard/hotkey_test.go 先例，
// 全程走桩件，不真注册系统热键。

// stubSheetHotkey hotkey.Backend 测试桩（与 msgboard 桩同形）：登记簿模拟
// OS 占用，regErr 可编程注入冲突。
type stubSheetHotkey struct {
	registered map[string]bool
	cbs        map[string]func()
	calls      []string
	regErr     func(accel string) error
}

func newStubSheetHotkey() *stubSheetHotkey {
	return &stubSheetHotkey{registered: map[string]bool{}, cbs: map[string]func(){}}
}

func (st *stubSheetHotkey) Register(accel string, cb func()) error {
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

func (st *stubSheetHotkey) Unregister(accel string) error {
	st.calls = append(st.calls, "unregister:"+accel)
	if !st.registered[accel] {
		return errors.New(`global shortcut "` + accel + `" is not registered`)
	}
	delete(st.registered, accel)
	delete(st.cbs, accel)
	return nil
}

func (st *stubSheetHotkey) IsRegistered(accel string) bool { return st.registered[accel] }

// newSheetService 组装"文件库模式 + 偏好 store 落临时目录"的服务（热键注册器
// 由调用方决定是否接线）。
func newSheetService(t *testing.T) *MemoService {
	t.Helper()
	root := t.TempDir()
	return &MemoService{
		files:    NewFileStore(filepath.Join(root, "memo")),
		useFiles: true,
		items:    []MemoItem{},
		holder:   extapi.NewLeaseHolder(ID),
		prefs:    newQuickMemoStore(root),
	}
}

func newWiredSheetService(t *testing.T) (*MemoService, *stubSheetHotkey) {
	t.Helper()
	s := newSheetService(t)
	st := newStubSheetHotkey()
	s.setHotkeyRegistry(hotkey.NewRegistry(st))
	return s, st
}

// start 即绑默认热键；stop 注销；再次 start 重绑（模块停用/重启收口）。
func TestSheetHotkeyStartStopRebind(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if !st.registered[defaultQuickHotkey] {
		t.Fatalf("start 后默认热键应在位: %v", st.calls)
	}
	if got := sheetStateOf(t, s); !got.HotkeyActive || got.Hotkey != defaultQuickHotkey {
		t.Fatalf("状态回显失真: %+v", got)
	}

	// OS 侧掉绑（等价启动期 pending 回滚）：实况上报不谎报。
	delete(st.registered, defaultQuickHotkey)
	if got := sheetStateOf(t, s); got.HotkeyActive {
		t.Fatal("系统未绑定时 HotkeyActive 不得谎报 true")
	}

	if err := s.stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if !st.registered[defaultQuickHotkey] {
		t.Fatalf("重启后必须重新绑定: %v", st.calls)
	}
}

// 改键成功：先注册新键、成功后才注销旧键；配置与实况同步。
func TestSheetHotkeyRebindSuccess(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetQuickSheetHotkey("Ctrl+Alt+M"); err != nil {
		t.Fatal(err)
	}
	// calls[0] 是 start 的首绑；换绑本身必须是"先注册新键、成功后才注销旧键"。
	seq := st.calls[1:]
	if len(seq) != 2 || seq[0] != "register:Ctrl+Alt+M" || seq[1] != "unregister:"+defaultQuickHotkey {
		t.Fatalf("换键调用序列失真（须先新后旧）: %v", st.calls)
	}
	if got := sheetStateOf(t, s); !got.HotkeyActive || got.Hotkey != "Ctrl+Alt+M" {
		t.Fatalf("状态回显失真: %+v", got)
	}
}

// 改键冲突：中文报错上抛、配置保持旧值、旧键全程未被注销仍然活着。
func TestSheetHotkeyRebindConflictKeepsOldKeyAlive(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	st.regErr = func(accel string) error {
		if accel == "Ctrl+Alt+J" {
			return errors.New(`the shortcut "Ctrl+Alt+J" is already registered (possibly by another application)`)
		}
		return nil
	}
	err := s.SetQuickSheetHotkey("Ctrl+Alt+J")
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("冲突应给改键指引: %v", err)
	}
	if got := s.prefs.Hotkey(); got != defaultQuickHotkey {
		t.Fatalf("冲突后配置应回滚 %q，实为 %q", defaultQuickHotkey, got)
	}
	if !st.registered[defaultQuickHotkey] {
		t.Fatalf("冲突后旧键必须仍在系统上活着: %v", st.registered)
	}
	for _, c := range st.calls {
		if c == "unregister:"+defaultQuickHotkey {
			t.Fatalf("冲突路径绝不允许先注销旧键: %v", st.calls)
		}
	}
}

// 停用热键：解绑幂等、配置落空、状态如实 inactive；停用态重启不复活。
func TestSheetHotkeyDisableUnbinds(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetQuickSheetHotkey(""); err != nil {
		t.Fatalf("停用热键不应报错: %v", err)
	}
	if len(st.registered) != 0 {
		t.Fatalf("停用后系统不应留有绑定: %v", st.registered)
	}
	if got := sheetStateOf(t, s); got.HotkeyActive || got.Hotkey != "" {
		t.Fatalf("停用状态失真: %+v", got)
	}
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

// 无修饰键的形态错误：不碰系统、不动配置。
func TestSheetHotkeyInvalidForm(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	before := len(st.calls)
	if err := s.SetQuickSheetHotkey("N"); err == nil || !strings.Contains(err.Error(), "修饰键") {
		t.Fatalf("应给形态校验中文指引: %v", err)
	}
	if len(st.calls) != before {
		t.Fatalf("校验失败不得触碰系统注册器: %v", st.calls)
	}
	if got := s.prefs.Hotkey(); got != defaultQuickHotkey {
		t.Fatalf("校验失败不得污染配置: %q", got)
	}
}

// 注册器未接线（独立跑测/装配前）：改键退化为纯配置读写，不报错。
func TestSetQuickHotkeyWithoutRegistry(t *testing.T) {
	s := newSheetService(t)
	if err := s.SetQuickSheetHotkey("Ctrl+Alt+K"); err != nil {
		t.Fatalf("未接线应退化为纯配置: %v", err)
	}
	if got := s.prefs.Hotkey(); got != "Ctrl+Alt+K" {
		t.Fatalf("配置未落: %q", got)
	}
}

// 回调接线：注册进系统的热键回调即唤卡入口（无头触发走服务层守卫，
// 不 panic、不进入已显示态、不留窗口记账）。
func TestSheetHotkeyCallbackWiredToToggle(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	cb := st.cbs[defaultQuickHotkey]
	if cb == nil {
		t.Fatal("注册回调缺失")
	}
	cb() // 无头：toggle→show 报"需应用运行"，仅记日志
	s.sheetMu.Lock()
	shown, win := s.sheetShown, s.sheetWin
	s.sheetMu.Unlock()
	if shown || win != nil {
		t.Fatalf("无头触发不得进入显示态: shown=%v win=%v", shown, win != nil)
	}
}

// stop 收口：在架空闲计时不再复活窗口（无头路径销毁即空操作、不 panic）。
func TestSheetStopHeadlessIdempotent(t *testing.T) {
	s, st := newWiredSheetService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if err := s.stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.stop(); err != nil {
		t.Fatalf("二次 stop 应幂等: %v", err)
	}
	if len(st.registered) != 0 {
		t.Fatalf("stop 后不应留有热键: %v", st.registered)
	}
	// stop 之后热键回调即便迟到也不得复活唤出（started=false 守卫）。
	if err := s.toggleQuickSheet(); err == nil {
		t.Fatal("stop 后 toggle 应报应用/生命周期守卫错误")
	}
}

// RPC 导出面门语义（Wave 3 口径回归）：holder 被门拒绝时速记方法如实上抛。
func TestSheetRPCGateCoverage(t *testing.T) {
	s, _ := newWiredSheetService(t)
	// 无头无窗：三方法各归各位——Show/Toggle 报应用未运行，Hide 幂等成功，
	// 状态读取正常回显。
	if err := s.ShowQuickSheet(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("ShowQuickSheet 无头应报可读错误: %v", err)
	}
	if err := s.ToggleQuickSheet(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("ToggleQuickSheet 无头应报可读错误: %v", err)
	}
	if err := s.HideQuickSheet(); err != nil {
		t.Fatalf("HideQuickSheet 未显示应幂等成功: %v", err)
	}
	_ = sheetStateOf(t, s)
}

// sheetStateOf 测试小件：经 RPC 导出版取速记卡状态（测试期 gate 未注入即放行）。
func sheetStateOf(t *testing.T, s *MemoService) QuickSheetState {
	t.Helper()
	st, err := s.GetQuickSheetState()
	if err != nil {
		t.Fatalf("GetQuickSheetState: %v", err)
	}
	return st
}
