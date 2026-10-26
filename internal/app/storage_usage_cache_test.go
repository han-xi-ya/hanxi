// storage_usage_cache_test.go 钉死存储度量 RPC 的锁分层纪律（A3 审查）：
// 缓存命中路径是纯内存短读，绝不排队等在途磁盘扫描之后；同时维持"并发只扫
// 一轮"的防惊群语义与 force 穿透语义不变。断言取外部可观察事实：
//   - 扫描被假件卡住在途时，新鲜缓存调用必须在短时限内返回（旧实现关在同一
//     把大锁里会连坐卡住，直接红）；
//   - 两个并发过期请求合并为一次底层扫描（调用计数经注入缝观察）；
//   - force 刷新永远重新扫描并回填缓存。
package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hanxi/internal/settings"
	"hanxi/packages/go/dirstats"
)

// withCleanUsageState 保存/还原全局缓存与扫描缝，保证串行用例互不污染。
func withCleanUsageState(t *testing.T) {
	t.Helper()
	// 先固化数据根（后续用例比对同一 root），再存环境。
	usageCacheMu.Lock()
	oldCache, oldAt, oldRoot, oldSub := usageCache, usageCacheAt, usageRoot, subUsageCache
	usageCacheMu.Unlock()
	oldMeasure := measureChildrenFn
	t.Cleanup(func() {
		usageCacheMu.Lock()
		usageCache, usageCacheAt, usageRoot, subUsageCache = oldCache, oldAt, oldRoot, oldSub
		usageCacheMu.Unlock()
		measureChildrenFn = oldMeasure
	})
	usageCacheMu.Lock()
	usageCache, usageCacheAt, usageRoot, subUsageCache = nil, time.Time{}, "", map[string]subCache{}
	usageCacheMu.Unlock()
}

type fakeScan struct {
	mu       sync.Mutex
	calls    int
	entered  chan struct{} // 缓冲，通知"扫描已进入"
	released chan struct{} // 关闭后放行；nil = 不阻塞
	items    []dirstats.Child
}

func (f *fakeScan) measure(string, dirstats.Options) ([]dirstats.Child, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	select {
	case f.entered <- struct{}{}:
	default:
	}
	if f.released != nil {
		<-f.released
	}
	return f.items, nil
}

func (f *fakeScan) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// callWithDeadline 在时限内跑 fn，超时即判失败（"没被卡住"的外部观察）。
func callWithDeadline(t *testing.T, timeout time.Duration, label string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("%s 在 %s 内未返回（缓存读被在途扫描连坐卡住，回归）", label, timeout)
	}
}

func TestDataRootUsageFreshCacheNotBlockedByScan(t *testing.T) {
	withCleanUsageState(t)
	root := settings.GetPaths().DataDir()

	// 先种一份新鲜缓存（模拟页面已进入、数据根未变）。
	seeded := []StorageUsageItem{{Name: "cached-dir", IsDir: true, Bytes: 42}}
	usageCacheMu.Lock()
	usageCache, usageCacheAt, usageRoot = seeded, time.Now(), root
	usageCacheMu.Unlock()

	enter, release := make(chan struct{}, 4), make(chan struct{})
	fake := &fakeScan{entered: enter, released: release,
		items: []dirstats.Child{{Name: "fresh-dir", IsDir: true, Stats: dirstats.Stats{Bytes: 7}}}}
	measureChildrenFn = fake.measure

	svc := &AppService{}
	// 后台发起 force 刷新：假扫描卡在盘上。
	scanDone := make(chan []StorageUsageItem, 1)
	go func() {
		items, err := svc.DataRootUsage(true)
		if err != nil {
			t.Errorf("force 扫描出错: %v", err)
		}
		scanDone <- items
	}()
	select {
	case <-enter:
	case <-time.After(2 * time.Second):
		t.Fatal("force 刷新未抵达扫描（前提不成立）")
	}

	// 外部可观察事实：扫描在途时，新鲜缓存读即时返回旧值、且完全不触扫描。
	var got []StorageUsageItem
	callWithDeadline(t, 2*time.Second, "DataRootUsage(force=false)", func() {
		items, err := svc.DataRootUsage(false)
		if err != nil {
			t.Errorf("缓存读出错: %v", err)
		}
		got = items
	})
	if len(got) != 1 || got[0].Name != "cached-dir" {
		t.Errorf("扫描在途时缓存读结果 = %+v, want 命中新鲜缓存 cached-dir", got)
	}
	if fake.count() != 1 {
		t.Errorf("扫描调用数 = %d, want 1（force 重新扫描、缓存读零扫描）", fake.count())
	}

	close(release)
	var fresh []StorageUsageItem
	select {
	case fresh = <-scanDone:
	case <-time.After(2 * time.Second):
		t.Fatal("放行后 force 扫描未收口")
	}
	if len(fresh) != 1 || fresh[0].Name != "fresh-dir" {
		t.Errorf("force 刷新结果 = %+v, want fresh-dir", fresh)
	}
	// 刷新后的缓存对后续读可见（零新增扫描）。
	again, err := svc.DataRootUsage(false)
	if err != nil || len(again) != 1 || again[0].Name != "fresh-dir" {
		t.Errorf("刷新后缓存读 = %+v, %v, want fresh-dir", again, err)
	}
	if fake.count() != 1 {
		t.Errorf("扫描调用数 = %d, want 1（TTL 内不再触盘）", fake.count())
	}
}

func TestDataRootUsageConcurrentStaleRequestsScanOnce(t *testing.T) {
	withCleanUsageState(t)

	enter, release := make(chan struct{}, 4), make(chan struct{})
	fake := &fakeScan{entered: enter, released: release,
		items: []dirstats.Child{{Name: "one-scan", IsDir: true, Stats: dirstats.Stats{Bytes: 99}}}}
	measureChildrenFn = fake.measure

	svc := &AppService{}
	type res struct {
		items []StorageUsageItem
		err   error
	}
	results := make(chan res, 2)
	launch := func() {
		go func() {
			items, err := svc.DataRootUsage(false)
			results <- res{items, err}
		}()
	}
	launch()
	select {
	case <-enter:
	case <-time.After(2 * time.Second):
		t.Fatal("首个请求未进入扫描")
	}
	launch() // 缓存过期且扫描在途：第二个请求应排队而非重复扫盘。
	close(release)

	seen := make([]string, 0, 2)
	for range 2 {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("DataRootUsage = %v", r.err)
			}
			if len(r.items) != 1 {
				t.Fatalf("结果异常: %+v", r.items)
			}
			seen = append(seen, r.items[0].Name)
		case <-time.After(2 * time.Second):
			t.Fatal("并发请求未在时限内收口")
		}
	}
	for _, name := range seen {
		if name != "one-scan" {
			t.Fatalf("并发结果不一致: %v", seen)
		}
	}
	if got := fake.count(); got != 1 {
		t.Fatalf("扫描调用数 = %d, want 1（防惊群：并发过期请求合并为一轮）", got)
	}
}

func TestDataRootSubUsageFreshCacheNotBlockedByRootScan(t *testing.T) {
	withCleanUsageState(t)
	root := settings.GetPaths().DataDir()
	sub, err := os.MkdirTemp(root, "a3sub-")
	if err != nil {
		t.Skipf("数据根不可写，无法构造子目录场景: %v", err)
	}
	subName := filepath.Base(sub)
	t.Cleanup(func() { _ = os.RemoveAll(sub) })

	seeded := []StorageSubUsageItem{{Name: "cached-sub", Bytes: 5}}
	usageCacheMu.Lock()
	subUsageCache[subName] = subCache{items: seeded, at: time.Now(), root: root}
	usageCacheMu.Unlock()

	enter, release := make(chan struct{}, 4), make(chan struct{})
	fake := &fakeScan{entered: enter, released: release}
	measureChildrenFn = fake.measure

	svc := &AppService{}
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		_, _ = svc.DataRootUsage(true) // 根扫描卡在盘上
	}()
	select {
	case <-enter:
	case <-time.After(2 * time.Second):
		t.Fatal("根扫描未进入")
	}

	var got []StorageSubUsageItem
	callWithDeadline(t, 2*time.Second, "DataRootSubUsage(缓存命中)", func() {
		items, err := svc.DataRootSubUsage(subName, false)
		if err != nil {
			t.Errorf("子级缓存读出错: %v", err)
		}
		got = items
	})
	if len(got) != 1 || got[0].Name != "cached-sub" {
		t.Errorf("子级缓存读 = %+v, want cached-sub", got)
	}
	if fake.count() != 1 {
		t.Errorf("扫描调用数 = %d, want 1（子级缓存命中不得触新扫描）", fake.count())
	}

	close(release)
	select {
	case <-scanDone:
	case <-time.After(2 * time.Second):
		t.Fatal("根扫描未收口")
	}
}
