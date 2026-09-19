package ocr

// 剪贴板识图热键：键位规范化、偏好持久化、服务层"先落系统再持久化/失败回滚"编排。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawHotkey 直改 ocr.json 里的 snipHotkey 字段（模拟手改坏值/老配置）。
func writeRawHotkey(t *testing.T, dir string, raw json.RawMessage) {
	t.Helper()
	path := filepath.Join(dir, "ocr.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		delete(m, "snipHotkey") // 老配置形态：字段缺席
	} else {
		m["snipHotkey"] = raw
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSnipHotkey(t *testing.T) {
	ok := map[string]string{
		"ctrl+alt+t":     "Ctrl+Alt+T",
		"Ctrl + Alt + T": "Ctrl+Alt+T",
		"alt+ctrl+r":     "Alt+Ctrl+R", // 修饰键序按录入序保留
		"ctrl+ctrl+x":    "Ctrl+X",     // 重复修饰键去重
		"win+d":          "Win+D",
		"cmd+opt+k":      "Win+Alt+K",
		"ctrl+f1":        "Ctrl+F1", // 带修饰的 F 键允许（纯 F 键才不开）
		"ctrl+shift+f24": "Ctrl+Shift+F24",
		"ctrl+alt+1":     "Ctrl+Alt+1",
		"ctrl+space":     "Ctrl+Space",
		"shift+escape":   "Shift+Escape",
	}
	for raw, want := range ok {
		got, err := NormalizeSnipHotkey(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeSnipHotkey(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	bad := []string{"", "  ", "t", "f1", "ctrl+", "ctrl+alt+", "hyper+x", "ctrl+alt+f99", "ctrl+中文"}
	for _, raw := range bad {
		if got, err := NormalizeSnipHotkey(raw); err == nil {
			t.Errorf("NormalizeSnipHotkey(%q) 应拒绝，实得 %q", raw, got)
		}
	}

	// 纯键位拒绝话术须点名"修饰键"，用户可据此改键
	if _, err := NormalizeSnipHotkey("F1"); err == nil || !strings.Contains(err.Error(), "修饰键") {
		t.Fatalf("纯 F 键拒绝话术失真: %v", err)
	}
}

func TestSnipHotkeyStorePersistence(t *testing.T) {
	dir := t.TempDir()
	s := newOcrStore(dir)
	if enabled, accel := s.GetSnipHotkey(); !enabled || accel != defaultSnipHotkey {
		t.Fatalf("默认应为 开 + %s，实得 %v %q", defaultSnipHotkey, enabled, accel)
	}

	if _, err := s.SetSnipHotkey("ctrl+alt+r"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSnipHotkeyEnabled(false); err != nil {
		t.Fatal(err)
	}
	// 重开句柄：持久化回读
	s2 := newOcrStore(dir)
	if enabled, accel := s2.GetSnipHotkey(); enabled || accel != "Ctrl+Alt+R" {
		t.Fatalf("持久化失真: %v %q", enabled, accel)
	}

	// 非法键位：拒绝且不改写现值
	if _, err := s2.SetSnipHotkey("f1"); err == nil {
		t.Fatal("纯 F 键应被拒绝")
	}
	if _, accel := s2.GetSnipHotkey(); accel != "Ctrl+Alt+R" {
		t.Fatalf("非法设定不得改值: %q", accel)
	}
}

func TestSnipHotkeyStoreCorruptValueFallback(t *testing.T) {
	dir := t.TempDir()
	s := newOcrStore(dir)
	if _, err := s.SetSnipHotkey("ctrl+alt+q"); err != nil {
		t.Fatal(err)
	}
	// 手改盘上键位为非法串 → 加载兜底默认（仿 listenPort 越界容忍）
	writeRawHotkey(t, dir, json.RawMessage(`"F1"`))
	s2 := newOcrStore(dir)
	if _, accel := s2.GetSnipHotkey(); accel != defaultSnipHotkey {
		t.Fatalf("非法持久化值应回落默认: %q", accel)
	}
	// 老配置（无热键字段）→ 默认值照常
	writeRawHotkey(t, dir, nil)
	s3 := newOcrStore(dir)
	if enabled, accel := s3.GetSnipHotkey(); !enabled || accel != defaultSnipHotkey {
		t.Fatalf("缺字段应给默认 开+默认键: %v %q", enabled, accel)
	}
}

// ---------- 服务层编排 ----------

// stubBinding ocr.SnipHotkeyBinding 桩：记录 Apply 调用，可编程失败。
type stubBinding struct {
	applied    []string // "accel|enabled" 序列
	failWith   error
	failAt     map[int]error
	registered bool
}

func (b *stubBinding) Apply(accel string, enabled bool) error {
	b.applied = append(b.applied, accel+"|"+boolStr(enabled))
	if err := b.failAt[len(b.applied)]; err != nil {
		return err
	}
	if b.failWith != nil {
		return b.failWith
	}
	b.registered = enabled
	return nil
}
func (b *stubBinding) Registered() bool { return b.registered }

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func TestSetSnipHotkeyRollbackOnBindFailure(t *testing.T) {
	s := newTestService(t, "")
	b := &stubBinding{failWith: errors.New("组合键 Ctrl+Alt+P 已被占用（可能被其他软件抢注），请到设置页改键")}
	s.SetSnipHotkeyBinding(b)

	if err := s.SetSnipHotkey("Ctrl+Alt+P"); err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("改键应透出占用错误: %v", err)
	}
	if _, accel := s.store.GetSnipHotkey(); accel != defaultSnipHotkey {
		t.Fatalf("注册失败须回滚配置: %q", accel)
	}

	b.failWith = nil
	if err := s.SetSnipHotkey("ctrl+alt+q"); err != nil {
		t.Fatal(err)
	}
	if _, accel := s.store.GetSnipHotkey(); accel != "Ctrl+Alt+Q" {
		t.Fatalf("改键成功应落账: %q", accel)
	}
	if len(b.applied) != 2 || b.applied[1] != "Ctrl+Alt+Q|1" {
		t.Fatalf("Apply 调用序列失真: %v", b.applied)
	}
}

func breakStoreSave(t *testing.T, s *ocrStore) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	s.filePath = filepath.Join(blocker, "ocr.json")
}

func TestSnipHotkeyStoreSetterRollbackOnSaveFailure(t *testing.T) {
	s := newOcrStore(t.TempDir())
	breakStoreSave(t, s)

	if _, err := s.SetSnipHotkey("Ctrl+Alt+R"); err == nil {
		t.Fatal("写盘失败应返回错误")
	}
	if enabled, accel := s.GetSnipHotkey(); !enabled || accel != defaultSnipHotkey {
		t.Fatalf("键位保存失败不得污染内存态: %v %q", enabled, accel)
	}
	if err := s.SetSnipHotkeyEnabled(false); err == nil {
		t.Fatal("开关写盘失败应返回错误")
	}
	if enabled, _ := s.GetSnipHotkey(); !enabled {
		t.Fatal("开关保存失败不得污染内存态")
	}
}

func TestSetSnipHotkeySaveFailureRollsBackBinding(t *testing.T) {
	s := newTestService(t, "")
	b := &stubBinding{registered: true}
	s.SetSnipHotkeyBinding(b)
	breakStoreSave(t, s.store)

	err := s.SetSnipHotkey("Ctrl+Alt+R")
	if err == nil {
		t.Fatal("保存失败应返回错误")
	}
	if got := strings.Join(b.applied, ","); got != "Ctrl+Alt+R|1,Ctrl+Alt+T|1" {
		t.Fatalf("保存失败应把系统换回旧键: %v", b.applied)
	}
	if enabled, accel := s.store.GetSnipHotkey(); !enabled || accel != defaultSnipHotkey {
		t.Fatalf("保存失败后三态配置应保持旧值: %v %q", enabled, accel)
	}
}

func TestSetSnipHotkeySaveAndBindingRollbackFailureJoined(t *testing.T) {
	s := newTestService(t, "")
	rollbackErr := errors.New("rollback binding failed")
	b := &stubBinding{registered: true, failAt: map[int]error{2: rollbackErr}}
	s.SetSnipHotkeyBinding(b)
	breakStoreSave(t, s.store)

	err := s.SetSnipHotkey("Ctrl+Alt+R")
	if err == nil || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "恢复全局热键旧键") {
		t.Fatalf("保存与补偿双失败应 errors.Join: %v", err)
	}
	if _, accel := s.store.GetSnipHotkey(); accel != defaultSnipHotkey {
		t.Fatalf("即使系统补偿失败，配置仍须保持旧键: %q", accel)
	}
}

func TestSetSnipHotkeyEnabledSaveFailureCompensates(t *testing.T) {
	s := newTestService(t, "")
	b := &stubBinding{registered: true}
	s.SetSnipHotkeyBinding(b)
	breakStoreSave(t, s.store)

	err := s.SetSnipHotkeyEnabled(false)
	if err == nil {
		t.Fatal("保存失败应返回错误")
	}
	if got := strings.Join(b.applied, ","); got != defaultSnipHotkey+"|0,"+defaultSnipHotkey+"|1" {
		t.Fatalf("开关保存失败应恢复系统旧态: %v", b.applied)
	}
	if enabled, _ := s.store.GetSnipHotkey(); !enabled {
		t.Fatal("开关保存失败后配置应保持开启")
	}
}

func TestSetSnipHotkeyEnabledSaveAndRollbackFailureJoined(t *testing.T) {
	s := newTestService(t, "")
	rollbackErr := errors.New("reenable failed")
	b := &stubBinding{registered: true, failAt: map[int]error{2: rollbackErr}}
	s.SetSnipHotkeyBinding(b)
	breakStoreSave(t, s.store)

	err := s.SetSnipHotkeyEnabled(false)
	if err == nil || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "恢复全局热键旧开关") {
		t.Fatalf("开关保存与补偿双失败应 errors.Join: %v", err)
	}
}

func TestSetSnipHotkeyEnabledAndState(t *testing.T) {
	s := newTestService(t, "")
	// 未注入通道：退化为纯配置读写，不报错
	if err := s.SetSnipHotkeyEnabled(false); err != nil {
		t.Fatal(err)
	}
	st, hotkeyErr := s.GetSnipHotkey()
	if hotkeyErr != nil {
		t.Fatal(hotkeyErr)
	}
	if st.Enabled || st.Accel != defaultSnipHotkey || st.Registered {
		t.Fatalf("无通道时状态失真: %+v", st)
	}

	b := &stubBinding{}
	s.SetSnipHotkeyBinding(b)
	if err := s.SetSnipHotkeyEnabled(true); err != nil {
		t.Fatal(err)
	}
	if b.applied[len(b.applied)-1] != defaultSnipHotkey+"|1" {
		t.Fatalf("开启应带当前键位落系统: %v", b.applied)
	}
	st, hotkeyErr = s.GetSnipHotkey()
	if hotkeyErr != nil {
		t.Fatal(hotkeyErr)
	}
	if !st.Enabled || !st.Registered {
		t.Fatalf("注册成功后状态失真: %+v", st)
	}
	if err := s.SetSnipHotkeyEnabled(false); err != nil {
		t.Fatal(err)
	}
	if b.applied[len(b.applied)-1] != defaultSnipHotkey+"|0" {
		t.Fatalf("关闭应通知注销: %v", b.applied)
	}
	if st, hotkeyErr := s.GetSnipHotkey(); hotkeyErr != nil || st.Registered {
		t.Fatal("禁用中 Registered 恒 false")
	}

	// 非法键位第一步即拒，不触系统
	before := len(b.applied)
	if err := s.SetSnipHotkey("T"); err == nil {
		t.Fatal("纯键位应拒绝")
	}
	if len(b.applied) != before {
		t.Fatal("非法键位不得触系统绑定")
	}
}
