//go:build windows

package windows

import (
	"fmt"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"
)

// fakeApplier 记录每次落点调用（scope 序列 + 发生的 OS 线程），可注入失败。
type fakeApplier struct {
	mu     sync.Mutex
	calls  []platform.KeepAwakeScope
	tids   []uint32
	failFn func(scope platform.KeepAwakeScope) error
}

func (f *fakeApplier) apply(scope platform.KeepAwakeScope) error {
	tid := windows.GetCurrentThreadId() // 必须在本线程实调时取（owner 独占线程合同的主角）
	f.mu.Lock()
	f.calls = append(f.calls, scope)
	f.tids = append(f.tids, tid)
	failFn := f.failFn
	f.mu.Unlock()
	if failFn != nil {
		return failFn(scope)
	}
	return nil
}

func (f *fakeApplier) snapshotCalls() []platform.KeepAwakeScope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.KeepAwakeScope(nil), f.calls...)
}

func (f *fakeApplier) snapshotTIDs() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint32(nil), f.tids...)
}

func assertCalls(t *testing.T, got []platform.KeepAwakeScope, want ...platform.KeepAwakeScope) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("落点调用序列长度 %d，期望 %d：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 次调用 scope=%d，期望 %d（序列 %v）", i, got[i], want[i], got)
		}
	}
}

// 多持有人交叠：并集生效，中间释放不撤，归零才撤销。
func TestKeepAwakeMultiHolderUnion(t *testing.T) {
	f := &fakeApplier{}
	k := newKeepAwake(f.apply)

	if err := k.Acquire("msgboard", platform.KeepAwakeSystem); err != nil {
		t.Fatalf("Acquire msgboard: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem)

	if err := k.Acquire("recordly", platform.KeepAwakeDisplay); err != nil {
		t.Fatalf("Acquire recordly: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem, platform.KeepAwakeSystem|platform.KeepAwakeDisplay)

	// 中间持有人离场：并集仍非零，不得出现清 0 调用。
	if err := k.Release("msgboard"); err != nil {
		t.Fatalf("Release msgboard: %v", err)
	}
	assertCalls(t, f.snapshotCalls(),
		platform.KeepAwakeSystem,
		platform.KeepAwakeSystem|platform.KeepAwakeDisplay,
		platform.KeepAwakeDisplay,
	)

	if got := k.Holders(); len(got) != 1 || got[0] != "recordly" {
		t.Fatalf("Holders=%v，期望 [recordly]", got)
	}

	// 最后一个持有人离场：归零清场恰好一次。
	if err := k.Release("recordly"); err != nil {
		t.Fatalf("Release recordly: %v", err)
	}
	last := f.snapshotCalls()
	assertCalls(t, last,
		platform.KeepAwakeSystem,
		platform.KeepAwakeSystem|platform.KeepAwakeDisplay,
		platform.KeepAwakeDisplay,
		0,
	)
	if got := k.Holders(); len(got) != 0 {
		t.Fatalf("Holders=%v，期望空", got)
	}
}

// 幂等语义：同 scope 重复 Acquire 不重放；未知/重复 Release 无操作。
func TestKeepAwakeIdempotent(t *testing.T) {
	f := &fakeApplier{}
	k := newKeepAwake(f.apply)

	for i := 0; i < 3; i++ {
		if err := k.Acquire("board", platform.KeepAwakeSystem|platform.KeepAwakeDisplay); err != nil {
			t.Fatalf("重复 Acquire #%d: %v", i, err)
		}
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem|platform.KeepAwakeDisplay)

	if err := k.Release("nobody"); err != nil {
		t.Fatalf("Release 未在场持有人应幂等成功: %v", err)
	}
	if err := k.Release("board"); err != nil {
		t.Fatalf("Release board: %v", err)
	}
	if err := k.Release("board"); err != nil {
		t.Fatalf("重复 Release 应幂等成功: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem|platform.KeepAwakeDisplay, 0)
}

// 改判语义：同持有人换 scope 覆盖旧诉求（不加计数，一次 Release 全撤）。
func TestKeepAwakeScopeReplace(t *testing.T) {
	f := &fakeApplier{}
	k := newKeepAwake(f.apply)

	if err := k.Acquire("board", platform.KeepAwakeSystem); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := k.Acquire("board", platform.KeepAwakeDisplay); err != nil {
		t.Fatalf("改判 Acquire: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem, platform.KeepAwakeDisplay)
	if err := k.Release("board"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem, platform.KeepAwakeDisplay, 0)
}

// 失败不吞状态：syscall 失败时 applied 不前进，后续变化自动重试；
// 归零失败时 owner 不退场（通道句柄保留），下一次投递打到同一 owner 重试。
func TestKeepAwakeFailureRetry(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	f := &fakeApplier{failFn: func(scope platform.KeepAwakeScope) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			return fmt.Errorf("模拟失败")
		}
		return nil
	}}
	k := newKeepAwake(f.apply)

	if err := k.Acquire("board", platform.KeepAwakeSystem); err == nil {
		t.Fatal("首次 Acquire 应把落点错误上抛")
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem)
	if got := k.Holders(); len(got) != 0 {
		t.Fatalf("Acquire 失败应回滚登记，Holders=%v", got)
	}

	// 失败的诉求不会潜伏进后续并集：下一次同步只反映在场持有人。
	if err := k.Acquire("other", platform.KeepAwakeDisplay); err != nil {
		t.Fatalf("第二次 Acquire 应成功: %v", err)
	}
	assertCalls(t, f.snapshotCalls(), platform.KeepAwakeSystem, platform.KeepAwakeDisplay)
	if err := k.Release("other"); err != nil {
		t.Fatalf("Release other: %v", err)
	}

	// 归零一次失败：owner 保留（失败不清退），下一次对账重试清 0 成功。
	f2 := &fakeApplier{failFn: func(scope platform.KeepAwakeScope) error {
		if scope == 0 {
			return fmt.Errorf("模拟清零失败")
		}
		return nil
	}}
	k2 := newKeepAwake(f2.apply)
	if err := k2.Acquire("a", platform.KeepAwakeSystem); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := k2.Release("a"); err == nil {
		t.Fatal("归零失败应上抛")
	}
	f2.mu.Lock()
	f2.failFn = nil
	f2.mu.Unlock()
	if err := k2.Release("a"); err != nil {
		t.Fatalf("重试归零应成功: %v", err)
	}
}

// 全部落点调用必须发生在同一条 OS 线程上（owner 独占线程合同）；
// 归零退场后再 Acquire 懒起新 owner，新周期内部仍然同线程。
func TestKeepAwakeOwnerSingleThread(t *testing.T) {
	f := &fakeApplier{}
	k := newKeepAwake(f.apply)

	steps := []func() error{
		func() error { return k.Acquire("a", platform.KeepAwakeSystem) },
		func() error { return k.Acquire("b", platform.KeepAwakeDisplay) },
		func() error { return k.Release("a") },
		func() error { return k.Release("b") }, // 归零，owner 退场
		func() error { return k.Acquire("c", platform.KeepAwakeSystem) },
		func() error { return k.Release("c") },
	}
	for i, fn := range steps {
		if err := fn(); err != nil {
			t.Fatalf("步骤 %d: %v", i, err)
		}
	}
	tids := f.snapshotTIDs()
	if len(tids) < 4 {
		t.Fatalf("落点调用过少：%v", tids)
	}
	// 第一段生命周期（前 4 次调用：置位、并集、缩并集、清 0）必须同线程。
	for i := 1; i < 4; i++ {
		if tids[i] != tids[0] {
			t.Fatalf("调用 %d 与调用 0 线程不同（%d vs %d）：owner 独占线程合同被破坏", i, tids[i], tids[0])
		}
	}
}

// 并发冒烟：多 goroutine 交叠 Acquire/Release 不死锁不丢账，全撤后干净归零。
func TestKeepAwakeConcurrent(t *testing.T) {
	f := &fakeApplier{}
	k := newKeepAwake(f.apply)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			holder := fmt.Sprintf("h%d", i)
			for j := 0; j < 20; j++ {
				_ = k.Acquire(holder, platform.KeepAwakeSystem|platform.KeepAwakeDisplay)
				_ = k.Release(holder)
			}
		}(i)
	}
	wg.Wait()

	if got := k.Holders(); len(got) != 0 {
		t.Fatalf("并发结束后仍有持有人：%v", got)
	}
	calls := f.snapshotCalls()
	if calls[len(calls)-1] != 0 {
		t.Fatalf("最后一次落点调用应为归零清场，实为 %v", calls[len(calls)-1])
	}
}
