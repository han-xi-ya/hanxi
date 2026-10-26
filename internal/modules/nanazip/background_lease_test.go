// background_lease_test.go 锁 nanazip 后台流程的 drain 门记账（ADR-0001 §1.4）。
//
// 背景：runInstall / runUninstall 由 RPC 方法 `go s.runInstall(...)` 派生，而派生它的
// InstallVersion/UninstallPackage 用的是 `holder.Enter()` + `defer release()`——方法一返回
// 租约就放了，后台的"下载 + 最长 15min 部署"全程**无人记账**，停用/退出的有界 drain 会当成
// "没有在途操作"直接收口。本测试用计数门把这个缺陷钉死：后台在途期间在途计数必须为 1。
//
// 断言对象是外部事实（门观察到的 Acquire/Release 次数与在途数），不是实现形状。
package nanazip

import (
	"context"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/nanazip/version"
	"hanxi/internal/platform/apppackage"
)

// countingGate 只记账不放行的门替身：普通 Gate 即可——LeaseHolder.EnterBackground 在
// 门未实现 BackgroundGate 时退回 Acquire 路径，因此后台租约同样落到这里的计数上。
type countingGate struct {
	mu       sync.Mutex
	acquires int
	releases int
	inFlight int
}

func (g *countingGate) Acquire(string) (func(), error) {
	var once sync.Once
	g.mu.Lock()
	g.acquires++
	g.inFlight++
	g.mu.Unlock()
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.inFlight--
			g.releases++
			g.mu.Unlock()
		})
	}, nil
}

func (g *countingGate) snapshot() (acquires, releases, inFlight int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.acquires, g.releases, g.inFlight
}

// blockingVersions 替身：EnsureCachedContext 被调用即通报，然后阻塞到测试放行或 ctx 取消
// ——模拟"一个正在进行的长下载"，让测试能在下载中途观察门的实况。
type blockingVersions struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once

	mu        sync.Mutex
	sawCancel bool
}

func (b *blockingVersions) ListReleases() ([]version.Release, error) {
	return []version.Release{{Version: "6.5.1800.0"}}, nil
}
func (b *blockingVersions) ListCached() ([]version.CachedPackage, error) { return nil, nil }
func (b *blockingVersions) EnsureCachedContext(ctx context.Context, v string, _ func(version.DownloadProgress)) (version.CachedPackage, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return version.CachedPackage{Version: v, Path: `C:\cache\NanaZip.msixbundle`}, nil
	case <-ctx.Done():
		b.mu.Lock()
		b.sawCancel = true
		b.mu.Unlock()
		return version.CachedPackage{}, ctx.Err()
	}
}
func (b *blockingVersions) RemoveCached(string) error { return nil }

// TestRunInstallHeldInDrainGate 后台安装流程在途期间，调用门必须数到一条在途租约；
// 收口后归零。**改造前这条必红**（runInstall 不取租约 → 在途恒 0）。
func TestRunInstallHeldInDrainGate(t *testing.T) {
	holder := extapi.NewLeaseHolder(ID)
	gate := &countingGate{}
	holder.SetGate(gate)

	blocker := &blockingVersions{entered: make(chan struct{}), release: make(chan struct{})}
	svc := newNanaZipService(nil, &fakePackages{}, blocker, holder)

	op := &operationState{id: "test-install", kind: "install", version: "6.5.1800.0"}
	done := make(chan struct{})
	go func() {
		svc.runInstall(op, true)
		close(done)
	}()

	// 等下载入口被真正调用（说明确实在途），而非按睡眠赌时序。
	select {
	case <-blocker.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("后台流程未进入下载入口（夹具或装配变了）")
	}

	if acquires, _, inFlight := gate.snapshot(); inFlight != 1 || acquires != 1 {
		t.Fatalf("后台在途期间门记账异常：acquires=%d inFlight=%d，期望 1/1——这正是『下载全程不计入 drain 门』的原缺陷", acquires, inFlight)
	}

	close(blocker.release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("放行后后台流程未收口")
	}

	if acquires, releases, inFlight := gate.snapshot(); inFlight != 0 || releases != 1 || acquires != 1 {
		t.Fatalf("收口后租约未归零：acquires=%d releases=%d inFlight=%d，期望 1/1/0", acquires, releases, inFlight)
	}
}

// TestRunUninstallHeldInDrainGate 卸载后台流程同样必须记账（同缺陷的第二处）。
func TestRunUninstallHeldInDrainGate(t *testing.T) {
	holder := extapi.NewLeaseHolder(ID)
	gate := &countingGate{}
	holder.SetGate(gate)

	pkg := &apppackage.Package{Version: "6.5.1800.0", PackageFullName: "full", Family: PackageFamily}
	fast := &fakePackages{pkg: pkg}
	svc := newNanaZipService(nil, fast, &blockingVersions{entered: make(chan struct{}), release: make(chan struct{})}, holder)

	op := &operationState{id: "test-uninstall", kind: "uninstall", version: "6.5.1800.0"}
	done := make(chan struct{})
	go func() {
		svc.runUninstall(op, "full")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("后台卸载未收口")
	}

	// 卸载链没有下载段，取租约后即刻走 packages.Uninstall/Query，
	// 收口时记账必须成对（Acquire 1 = Release 1），且不留残账。
	if acquires, releases, inFlight := gate.snapshot(); acquires < 1 || releases != acquires || inFlight != 0 {
		t.Fatalf("卸载流程租约不成对：acquires=%d releases=%d inFlight=%d", acquires, releases, inFlight)
	}
}
