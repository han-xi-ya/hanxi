package hotkey

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubBackend 可编程底层：登记簿模拟 OS 占用，regErr 按需注入冲突。
type stubBackend struct {
	mu         sync.Mutex
	registered map[string]bool
	calls      []string
	regErr     func(accel string) error
	unregErr   func(accel string) error
	regBlock   func(accel string)
}

func newStub() *stubBackend {
	return &stubBackend{registered: map[string]bool{}}
}

func (s *stubBackend) Register(accel string, cb func()) error {
	if s.regBlock != nil {
		s.regBlock(accel)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "register:"+accel)
	if s.regErr != nil {
		if err := s.regErr(accel); err != nil {
			return err
		}
	}
	if s.registered[accel] {
		return errors.New(`the shortcut is already registered (possibly by another application)`)
	}
	s.registered[accel] = true
	return nil
}

func (s *stubBackend) Unregister(accel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "unregister:"+accel)
	if s.unregErr != nil {
		if err := s.unregErr(accel); err != nil {
			return err
		}
	}
	if !s.registered[accel] {
		return errors.New(`global shortcut "` + accel + `" is not registered`)
	}
	delete(s.registered, accel)
	return nil
}

func (s *stubBackend) IsRegistered(accel string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registered[accel]
}

func (s *stubBackend) isRegistered(accel string) bool { return s.IsRegistered(accel) }

func (s *stubBackend) callSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestBindBasics(t *testing.T) {
	st := newStub()
	r := NewRegistry(st)
	h := func() {}

	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	if !r.Registered("demo") || r.Accel("demo") != "Ctrl+Alt+T" {
		t.Fatalf("首绑后应注册成功: %+v", st.calls)
	}

	// 同槽同键：幂等 no-op，不再触底层
	before := len(st.calls)
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	if len(st.calls) != before {
		t.Fatalf("同键重放不应触底层: %v", st.calls[before:])
	}

	// 参数闸门
	if err := r.Bind("", "Ctrl+T", true, h); err == nil {
		t.Fatal("空槽位名应拒绝")
	}
	if err := r.Bind("demo", "  ", true, h); err == nil {
		t.Fatal("开状态下空键位应拒绝")
	}
	if err := r.Bind("demo", "Ctrl+T", true, nil); err == nil {
		t.Fatal("空回调应拒绝")
	}
}

func TestRebindSuccessAndConflict(t *testing.T) {
	st := newStub()
	st.regErr = func(accel string) error {
		if accel == "Ctrl+Alt+Y" {
			return errors.New("the shortcut is already registered (possibly by another application)")
		}
		return nil
	}
	r := NewRegistry(st)
	h := func() {}
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}

	// 冲突：新键注册失败 → 旧键保持、槽位记账不变、中文可读错误
	err := r.Bind("demo", "Ctrl+Alt+Y", true, h)
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("冲突应给改键指引: %v", err)
	}
	if r.Accel("demo") != "Ctrl+Alt+T" || !r.Registered("demo") {
		t.Fatal("冲突失败后旧绑定必须原样保留")
	}

	// 换键成功：先注册新、后注销旧（顺序即安全语义）
	if err := r.Bind("demo", "Ctrl+Alt+R", true, h); err != nil {
		t.Fatal(err)
	}
	want := []string{"register:Ctrl+Alt+R", "unregister:Ctrl+Alt+T"}
	if strings.Join(st.calls[2:], ",") != strings.Join(want, ",") {
		t.Fatalf("换键调用序列失真: %v", st.calls)
	}
	if r.Accel("demo") != "Ctrl+Alt+R" || st.registered["Ctrl+Alt+T"] {
		t.Fatalf("换键后状态失真: %v", st.registered)
	}
}

func TestUnbindAndDisabled(t *testing.T) {
	st := newStub()
	r := NewRegistry(st)
	h := func() {}
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}

	if err := r.Bind("demo", "Ctrl+Alt+T", false, nil); err != nil {
		t.Fatal(err)
	}
	if r.Registered("demo") || st.registered["Ctrl+Alt+T"] {
		t.Fatal("enabled=false 应解绑")
	}
	// 幂等：再关一次不报错
	if err := r.Unbind("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestStartupRollbackRecovery(t *testing.T) {
	// 模拟启动期 pending 绑定被 OS 拒绝后的残账：slots 记着键位、系统其实没绑。
	st := newStub()
	r := NewRegistry(st)
	h := func() {}
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	delete(st.registered, "Ctrl+Alt+T") // flushPending 回滚

	if r.Registered("demo") {
		t.Fatal("实况以底层为准：OS 未绑定时应报 false")
	}
	// 同键重试必须真重发 Register（不被"幂等"吞掉）
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	if !st.registered["Ctrl+Alt+T"] {
		t.Fatal("重试应重新注册")
	}
	// 解绑残账：底层 Unregister 报"not registered"也视为达成
	delete(st.registered, "Ctrl+Alt+T")
	if err := r.Unbind("demo"); err != nil {
		t.Fatalf("解绑残账应容忍失败: %v", err)
	}
}

func TestConcurrentBindSameSlotIsSerialized(t *testing.T) {
	st := newStub()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	st.regBlock = func(accel string) {
		if accel == "Ctrl+Alt+T" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
	}
	r := NewRegistry(st)
	h := func() {}

	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- r.Bind("demo", "Ctrl+Alt+T", true, h) }()
	<-entered
	go func() { second <- r.Bind("demo", "Ctrl+Alt+T", true, h) }()

	select {
	case err := <-second:
		t.Fatalf("同槽第二次绑定越过首事务提前返回: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	calls := st.callSnapshot()
	if got := strings.Join(calls, ","); got != "register:Ctrl+Alt+T" {
		t.Fatalf("并发同槽同键只能注册一次，实得 %v", calls)
	}
	if !r.Registered("demo") || r.Accel("demo") != "Ctrl+Alt+T" {
		t.Fatal("并发绑定后槽位与系统态应一致")
	}
}

func TestUnbindFailureKeepsRetryCredential(t *testing.T) {
	st := newStub()
	r := NewRegistry(st)
	if err := r.Bind("demo", "Ctrl+Alt+T", true, func() {}); err != nil {
		t.Fatal(err)
	}
	fail := true
	st.unregErr = func(string) error {
		if fail {
			return errors.New("injected unregister failure")
		}
		return nil
	}

	if err := r.Unbind("demo"); err == nil {
		t.Fatal("首次注销失败应返回错误")
	}
	if r.Accel("demo") != "Ctrl+Alt+T" || !r.Registered("demo") {
		t.Fatal("注销失败后必须保留槽位凭据供二次重试")
	}
	fail = false
	if err := r.Unbind("demo"); err != nil {
		t.Fatalf("二次注销应使用保留凭据成功: %v", err)
	}
	if r.Accel("demo") != "" || r.Registered("demo") || st.isRegistered("Ctrl+Alt+T") {
		t.Fatal("二次注销成功后槽位与系统态都应清空")
	}
}

func TestRebindOldUnregisterFailureRollsBackNew(t *testing.T) {
	st := newStub()
	r := NewRegistry(st)
	h := func() {}
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	st.unregErr = func(accel string) error {
		if accel == "Ctrl+Alt+T" {
			return errors.New("old unregister failed")
		}
		return nil
	}

	err := r.Bind("demo", "Ctrl+Alt+R", true, h)
	if err == nil || !strings.Contains(err.Error(), "旧键") {
		t.Fatalf("旧键注销失败应明确报错: %v", err)
	}
	if r.Accel("demo") != "Ctrl+Alt+T" || !r.Registered("demo") {
		t.Fatal("换绑失败后槽位必须回滚旧键")
	}
	if !st.isRegistered("Ctrl+Alt+T") || st.isRegistered("Ctrl+Alt+R") {
		t.Fatal("换绑失败后系统态必须只保留旧键")
	}
	want := "register:Ctrl+Alt+R,unregister:Ctrl+Alt+T,unregister:Ctrl+Alt+R"
	calls := st.callSnapshot()
	if got := strings.Join(calls[len(calls)-3:], ","); got != want {
		t.Fatalf("失败补偿序列失真: %v", calls)
	}
}

func TestRebindRollbackFailureIsRetried(t *testing.T) {
	st := newStub()
	r := NewRegistry(st)
	h := func() {}
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatal(err)
	}
	failOld, failNew := true, true
	st.unregErr = func(accel string) error {
		if accel == "Ctrl+Alt+T" && failOld {
			return errors.New("old unregister failed")
		}
		if accel == "Ctrl+Alt+R" && failNew {
			return errors.New("rollback unregister failed")
		}
		return nil
	}

	err := r.Bind("demo", "Ctrl+Alt+R", true, h)
	if err == nil || !strings.Contains(err.Error(), "补偿") {
		t.Fatalf("双失败应同时报告补偿失败: %v", err)
	}
	if r.Accel("demo") != "Ctrl+Alt+T" || !st.isRegistered("Ctrl+Alt+T") || !st.isRegistered("Ctrl+Alt+R") {
		t.Fatal("补偿失败时配置槽仍指旧键，并显式保留新键待清理")
	}

	failOld, failNew = false, false
	if err := r.Bind("demo", "Ctrl+Alt+T", true, h); err != nil {
		t.Fatalf("重放旧配置应先清理 pending 新键: %v", err)
	}
	if st.isRegistered("Ctrl+Alt+R") || !st.isRegistered("Ctrl+Alt+T") {
		t.Fatal("重试后系统态应恢复为仅旧键")
	}
}

func TestUserErrorMapping(t *testing.T) {
	if got := userError("F1", errors.New("key \"f1\" is not supported as a global shortcut on Windows")); !strings.Contains(got.Error(), "注册失败") {
		t.Fatalf("非冲突错误应保留原文: %v", got)
	}
}
