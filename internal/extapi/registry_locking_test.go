// registry_locking_test.go 钉死 A3 审查落地的三条装配纪律，断言全部取"外部
// 可观察事实"而非实现细节：
//  1. Register 批量失败不留半注册状态（观察面 = 失败后 List/Acquire 的真实内容）；
//  2. 凭据磁盘读与模块代码回调不得在 wrapper.mu 内执行——观察面 = 它们卡住期间，
//     启停门（SetEnabled）与状态读取（IsEnabled/IsActive）仍然可用。真实现的
//     ReceiptStorage.IsInstalled 是磁盘读（settings.ReceiptStore 逐文件 Read），
//     持锁执行会把该模块的 Acquire/停用全部冻住；
//  3. 并发 Acquire 共用懒激活（初始化恰好一次），全部 release 后租约计数真的归零
//     ——观察面 = 投影从 busy 收敛回 active、停用 drain 立即完成、OnDestroy 恰好一次。
package extapi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 失格模块替身：Info/Nav/TrayCommands 可分别被闸门卡住（卡点前先落"已进入"哨兵；
// 进入哨兵出现在模块方法内，意味着调用方快照段已收尾——对旧实现该哨兵是在
// wrapper.mu 内发出的，探针因此会卡死，红得其所）。
// ---------------------------------------------------------------------------

type a3GatedModule struct {
	id   string
	navs []NavEntry

	blockInfo, blockNav, blockTray atomic.Bool
	infoStuck, navStuck, trayStuck chan struct{}
	infoBlock, navBlock, trayBlock chan struct{}

	initCount  atomic.Int32
	destroyCnt atomic.Int32
}

func newA3GatedModule(id string) *a3GatedModule {
	return &a3GatedModule{
		id:        id,
		infoStuck: make(chan struct{}, 8),
		navStuck:  make(chan struct{}, 8),
		trayStuck: make(chan struct{}, 8),
		infoBlock: make(chan struct{}),
		navBlock:  make(chan struct{}),
		trayBlock: make(chan struct{}),
	}
}

func (m *a3GatedModule) enter(stuck chan struct{}, block chan struct{}, armed *atomic.Bool) {
	if !armed.Load() {
		return
	}
	stuck <- struct{}{}
	<-block
}

func (m *a3GatedModule) Info() ModuleInfo {
	m.enter(m.infoStuck, m.infoBlock, &m.blockInfo)
	return ModuleInfo{ID: m.id, Name: "A3 " + m.id, Level: LevelBuiltin}
}
func (m *a3GatedModule) Nav() []NavEntry {
	m.enter(m.navStuck, m.navBlock, &m.blockNav)
	return m.navs
}
func (m *a3GatedModule) Services() []Service { return nil }
func (m *a3GatedModule) OnInit(context.Context) error {
	m.initCount.Add(1)
	return nil
}
func (m *a3GatedModule) OnDestroy() error { m.destroyCnt.Add(1); return nil }
func (m *a3GatedModule) IsInitialized() bool {
	return m.initCount.Load() > m.destroyCnt.Load()
}
func (m *a3GatedModule) TrayCommands() []TrayCommand {
	m.enter(m.trayStuck, m.trayBlock, &m.blockTray)
	return []TrayCommand{{ID: "run", Label: "run", Run: func(context.Context) error { return nil }}}
}

// ---------------------------------------------------------------------------
// 卡住的凭据存储：首次 IsInstalled 停在"盘上"（模拟真 ReceiptStore 的文件读）
// ---------------------------------------------------------------------------

type a3BlockingReceipts struct {
	entered chan struct{}
	block   chan struct{}
	once    sync.Once
}

func (s *a3BlockingReceipts) IsInstalled(string) bool {
	s.once.Do(func() { s.entered <- struct{}{} })
	<-s.block
	return true
}
func (s *a3BlockingReceipts) MarkInstalled(string, ReceiptKind) error { return nil }
func (s *a3BlockingReceipts) MarkAbsent(string) error                 { return nil }
func (s *a3BlockingReceipts) Installed() map[string]bool              { return map[string]bool{} }

// mustSettle 在 timeout 内跑完 fn 即视为"未被冻住"；超时即失败并给出标签。
func mustSettle(t *testing.T, timeout time.Duration, label string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("%s 在 %s 内未返回：读路径在 wrapper 锁内执行了模块回调/凭据 IO（回归）", label, timeout)
	}
}

// stuckProbe 场景骨架：枚举卡在模块回调期间探针必须可用，收尾放行并回收枚举。
func stuckProbe(t *testing.T, reg *Registry, moduleID string, stuck chan struct{}, block chan struct{}, enum func()) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Go(enum)
	select {
	case <-stuck:
	case <-time.After(2 * time.Second):
		t.Fatal("枚举路径未抵达模块回调卡点（前提不成立）")
	}
	mustSettle(t, 2*time.Second, "IsEnabled("+moduleID+")", func() { _ = reg.IsEnabled(moduleID) })
	mustSettle(t, 2*time.Second, "IsActive("+moduleID+")", func() { _ = reg.IsActive(moduleID) })
	mustSettle(t, 2*time.Second, "SetEnabled("+moduleID+", false)", func() {
		if err := reg.SetEnabled(moduleID, false); err != nil {
			t.Errorf("SetEnabled(false) = %v", err)
		}
	})
	close(block)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// 1. Register 全有或全无
// ---------------------------------------------------------------------------

func TestRegisterBatchIsAtomic(t *testing.T) {
	t.Run("批内一条空 ID：整批不落", func(t *testing.T) {
		reg := NewRegistry(nil)
		good := newRegistryTestModule("good")
		if err := reg.Register(good, newRegistryTestModule("")); err == nil {
			t.Fatal("含空 ID 的批次必须整体报错")
		}
		if infos := reg.List(); len(infos) != 0 {
			t.Fatalf("失败批次后注册表应全空，实际含 %d 项（半注册泄露）", len(infos))
		}
		if _, _, err := reg.Acquire("good"); !errors.Is(err, ErrUnknownModule) {
			t.Fatalf("失败批次里的合法模块不应可调用，err = %v", err)
		}
	})

	t.Run("批内自撞 ID：整批不落", func(t *testing.T) {
		reg := NewRegistry(nil)
		if err := reg.Register(newRegistryTestModule("dup"), newRegistryTestModule("dup")); err == nil {
			t.Fatal("批内重复 ID 必须整体报错")
		}
		if infos := reg.List(); len(infos) != 0 {
			t.Fatalf("失败批次后注册表应全空，实际含 %d 项", len(infos))
		}
	})

	t.Run("撞已注册：新条目不落、旧条目原样", func(t *testing.T) {
		reg := NewRegistry(nil)
		first := newRegistryTestModule("first")
		if err := reg.Register(first); err != nil {
			t.Fatal(err)
		}
		fresh := newRegistryTestModule("fresh")
		if err := reg.Register(fresh, newRegistryTestModule("first")); err == nil {
			t.Fatal("与已注册 ID 冲突必须整体报错")
		}
		infos := reg.List()
		if len(infos) != 1 || infos[0].ID != "first" {
			t.Fatalf("失败批次不得留下 fresh；注册表现状 = %d 项 %+v", len(infos), infos)
		}
		if got := fresh.initCount.Load(); got != 0 {
			t.Fatal("未入账模块不应被任何路径激活")
		}
	})
}

// ---------------------------------------------------------------------------
// 2. 枚举/投影读路径不持 wrapper 锁执行模块回调与凭据 IO
// ---------------------------------------------------------------------------

func TestEnumerationReadsNeverFreezeLifecycleGate(t *testing.T) {
	t.Run("List 卡在 Info 时启停门仍可用", func(t *testing.T) {
		reg := NewRegistry(nil)
		mod := newA3GatedModule("gated")
		if err := reg.Register(mod); err != nil {
			t.Fatal(err)
		}
		mod.blockInfo.Store(true)
		stuckProbe(t, reg, "gated", mod.infoStuck, mod.infoBlock, func() { _ = reg.List() })
	})

	t.Run("ListStates 卡在 Info 时启停门仍可用", func(t *testing.T) {
		reg := NewRegistry(nil)
		mod := newA3GatedModule("gated")
		if err := reg.Register(mod); err != nil {
			t.Fatal(err)
		}
		mod.blockInfo.Store(true)
		stuckProbe(t, reg, "gated", mod.infoStuck, mod.infoBlock, func() { _ = reg.ListStates() })
	})

	t.Run("GetEnabledNavs 卡在 Nav 时启停门仍可用", func(t *testing.T) {
		reg := NewRegistry(nil)
		mod := newA3GatedModule("gated")
		mod.navs = []NavEntry{{ID: "gated-nav", Route: "/gated", Section: SectionExt}}
		if err := reg.Register(mod); err != nil {
			t.Fatal(err)
		}
		mod.blockNav.Store(true)
		stuckProbe(t, reg, "gated", mod.navStuck, mod.navBlock, func() { _ = reg.GetEnabledNavs() })
	})

	t.Run("ListTrayCommands 卡在 TrayCommands 时启停门仍可用", func(t *testing.T) {
		reg := NewRegistry(nil)
		mod := newA3GatedModule("gated")
		if err := reg.Register(mod); err != nil {
			t.Fatal(err)
		}
		mod.blockTray.Store(true)
		stuckProbe(t, reg, "gated", mod.trayStuck, mod.trayBlock, func() { _ = reg.ListTrayCommands() })
	})

	t.Run("凭据读卡住时启停门仍可用", func(t *testing.T) {
		reg := NewRegistry(nil)
		mod := newRegistryTestModule("demo")
		if err := reg.Register(mod); err != nil {
			t.Fatal(err)
		}
		blocking := &a3BlockingReceipts{entered: make(chan struct{}, 1), block: make(chan struct{})}
		reg.SetReceiptStorage(blocking)

		var wg sync.WaitGroup
		wg.Go(func() { _ = reg.ListTrayCommands() })
		select {
		case <-blocking.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("ListTrayCommands 未走到凭据判定（前提不成立）")
		}
		mustSettle(t, 2*time.Second, "IsActive(demo)", func() { _ = reg.IsActive("demo") })
		mustSettle(t, 2*time.Second, "SetEnabled(demo, false)", func() {
			if err := reg.SetEnabled("demo", false); err != nil {
				t.Errorf("SetEnabled(false) = %v", err)
			}
		})
		close(blocking.block)
		wg.Wait()
	})
}

// ---------------------------------------------------------------------------
// 3. 并发 Acquire：懒激活恰好一次，release 全数归零可观察
// ---------------------------------------------------------------------------

func TestConcurrentAcquireInitializesOnceAndLeasesSettleToZero(t *testing.T) {
	reg := NewRegistry(nil)
	mod := newA3GatedModule("leased")
	if err := reg.Register(mod); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	var start, finish sync.WaitGroup
	start.Add(workers)
	finish.Add(workers)
	errs := make(chan error, workers)
	releases := make(chan func(), workers)
	for range workers {
		go func() {
			defer finish.Done()
			start.Done()
			start.Wait() // 尽量同刻冲门
			_, release, err := reg.Acquire("leased")
			if err != nil {
				errs <- err
				return
			}
			releases <- release
		}()
	}
	finish.Wait()
	close(errs)
	close(releases)
	for err := range errs {
		t.Fatalf("并发 Acquire 失败: %v", err)
	}

	// 外部可观察事实 1：并发 16 路取门，OnInit 恰好跑一次。
	if got := mod.initCount.Load(); got != 1 {
		t.Fatalf("OnInit count = %d, want 1（懒激活并发只初始化一次）", got)
	}

	all := make([]func(), 0, workers)
	for fn := range releases {
		all = append(all, fn)
	}
	if len(all) != workers {
		t.Fatalf("取得 lease 数 = %d, want %d", len(all), workers)
	}
	// 持有期投影必为 busy（全部在飞计数入账后才统一释放）。
	if st := stateFor(t, reg, "leased"); st.Runtime != RuntimeBusy {
		t.Fatalf("持有期 runtime = %s, want busy", st.Runtime)
	}
	for _, fn := range all {
		fn()
	}
	// 外部可观察事实 2：租约真的归零——投影从 busy 收敛回 active，
	// 停用 drain 立刻完成（不再等待幽灵计数），OnDestroy 恰好一次。
	waitRuntime(t, reg, "leased", RuntimeActive, 2*time.Second)
	startAt := time.Now()
	if err := reg.SetEnabled("leased", false); err != nil {
		t.Fatalf("SetEnabled(false) = %v", err)
	}
	if elapsed := time.Since(startAt); elapsed > 2*time.Second {
		t.Fatalf("全部释放后停用耗时 %s，疑似租约计数未归零（drain 空等）", elapsed)
	}
	if got := mod.destroyCnt.Load(); got != 1 {
		t.Fatalf("OnDestroy count = %d, want 1", got)
	}
	if st := stateFor(t, reg, "leased"); st.Runtime != RuntimeInactive {
		t.Fatalf("收口后 runtime = %s, want inactive", st.Runtime)
	}
}
