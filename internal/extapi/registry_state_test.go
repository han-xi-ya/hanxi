// registry_state_test.go 覆盖 Wave 1 Registry 侧契约实现：ListStates 四维投影、
// Acquire 统一调用门与 operation lease、OnLifecycle 多播钩子。
// 复用 registry_test.go 的 fake 模块/存储风格。
package extapi

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// testReceipts 内存版 ReceiptStorage：驱动 Delivery 维度与调用门。
type testReceipts struct {
	mu        sync.Mutex
	installed map[string]bool
}

func newTestReceipts(ids ...string) *testReceipts {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return &testReceipts{installed: set}
}

func (s *testReceipts) IsInstalled(moduleID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installed[moduleID]
}

func (s *testReceipts) MarkInstalled(moduleID string, _ ReceiptKind) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installed[moduleID] = true
	return nil
}

func (s *testReceipts) MarkAbsent(moduleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.installed, moduleID)
	return nil
}

func (s *testReceipts) Installed() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.installed))
	for id, ok := range s.installed {
		out[id] = ok
	}
	return out
}

// stateFor 取出指定模块的投影；不存在即判失败。
func stateFor(t *testing.T, r *Registry, id string) ModuleState {
	t.Helper()
	for _, s := range r.ListStates() {
		if s.ModuleID == id {
			return s
		}
	}
	t.Fatalf("module %q missing from ListStates", id)
	return ModuleState{}
}

func eq[T comparable](t *testing.T, label string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// waitRuntime 轮询等待运行维度抵达期望值（drain 等异步收敛场景）。
func waitRuntime(t *testing.T, r *Registry, id string, want RuntimeState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if stateFor(t, r, id).Runtime == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime of %q did not reach %v within %s (last=%v)",
				id, want, timeout, stateFor(t, r, id).Runtime)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// ListStates 投影
// ---------------------------------------------------------------------------

func TestListStatesProjection(t *testing.T) {
	reg := NewRegistry(nil)
	demo := newRegistryTestModule("demo")
	demo.navs = []NavEntry{{ID: "demo-nav", Route: "/demo", Section: SectionExt, Order: 10}}
	aux := newRegistryTestModule("aaa")
	if err := reg.Register(demo, aux); err != nil {
		t.Fatal(err)
	}

	states := reg.ListStates()
	if len(states) != 2 {
		t.Fatalf("ListStates len = %d, want 2", len(states))
	}
	if !slices.IsSortedFunc(states, func(a, b ModuleState) int {
		return strings.Compare(a.ModuleID, b.ModuleID)
	}) {
		t.Errorf("ListStates 未按 ModuleID 稳定排序: %v", enumStringsOf(states))
	}
	if states[0].ModuleID != "aaa" || states[1].ModuleID != "demo" {
		t.Fatalf("ListStates order = [%s %s], want [aaa demo]", states[0].ModuleID, states[1].ModuleID)
	}

	// 未注入 receipts：兼容旧装配路径，全部按 installed/enabled 投影。
	for _, s := range states {
		eq(t, s.ModuleID+".delivery", s.Delivery, DeliveryInstalled)
		eq(t, s.ModuleID+".policy", s.Policy, PolicyEnabled)
		eq(t, s.ModuleID+".runtime", s.Runtime, RuntimeInactive)
		eq(t, s.ModuleID+".health", s.Health, HealthCurrent)
		eq(t, s.ModuleID+".action", s.PrimaryAction, ActionOpen)
		eq(t, s.ModuleID+".summary", s.Summary, SummaryIdleEnabled)
		eq(t, s.ModuleID+".schema", s.Schema, ModuleContractSchema)
	}
	// receipts 未注入时 IsInstalled 恒 true，List() 兼容字段同步为 installed。
	for _, info := range reg.List() {
		eq(t, "List("+info.ID+").Installed", info.Installed, true)
	}
	if !reg.IsInstalled("demo") {
		t.Error(`IsInstalled("demo") = false, want true（未注入 receipts 恒已安装）`)
	}

	// 注入 receipt 存储但 demo 缺凭据 → absent/install。
	receipts := newTestReceipts("aaa")
	reg.SetReceiptStorage(receipts)
	s := stateFor(t, reg, "demo")
	eq(t, "demo.delivery", s.Delivery, DeliveryAbsent)
	eq(t, "demo.action", s.PrimaryAction, ActionInstall)
	eq(t, "demo.summary", s.Summary, SummaryNotInstalled)
	if s.Reason != "" {
		t.Errorf("demo.Reason = %q, want empty for install", s.Reason)
	}
	// aaa 有凭据，仍按 installed 基线投影。
	eq(t, "aaa.delivery", stateFor(t, reg, "aaa").Delivery, DeliveryInstalled)

	// Wave 1 统一口径：未安装不进调用门、不懒激活、不进导航与托盘候选。
	if reg.IsInstalled("demo") {
		t.Error(`IsInstalled("demo") = true, want false（缺凭据）`)
	}
	if !reg.IsInstalled("aaa") {
		t.Error(`IsInstalled("aaa") = false, want true（有凭据）`)
	}
	for _, info := range reg.List() {
		eq(t, "List("+info.ID+").Installed", info.Installed, info.ID == "aaa")
	}
	if err := reg.EnsureActive("demo"); !errors.Is(err, ErrModuleNotInstalled) {
		t.Fatalf("EnsureActive not-installed error = %v, want ErrModuleNotInstalled", err)
	}
	if got := demo.initCount.Load(); got != 0 {
		t.Fatalf("OnInit count = %d, want 0（receipt 门必须先于懒激活拒绝）", got)
	}
	if navs := reg.GetEnabledNavs(); len(navs) != 0 {
		t.Errorf("GetEnabledNavs = %v, want empty（未安装不进导航）", navs)
	}
	hasDemoCmd := func() bool {
		return slices.ContainsFunc(reg.ListTrayCommands(), func(c TrayCommandInfo) bool {
			return c.Key == "demo/run"
		})
	}
	if hasDemoCmd() {
		t.Error("ListTrayCommands 含 demo/run，want 不含（未安装不进托盘候选）")
	}

	// 补登记 demo 凭据 → 回到 installed 基线，各入口恢复。
	if err := receipts.MarkInstalled("demo", ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	eq(t, "demo.delivery", stateFor(t, reg, "demo").Delivery, DeliveryInstalled)
	eq(t, "demo.action", stateFor(t, reg, "demo").PrimaryAction, ActionOpen)
	if navs := reg.GetEnabledNavs(); len(navs) != 1 || navs[0].ID != "demo-nav" {
		t.Errorf("GetEnabledNavs after install = %v, want [demo-nav]", navs)
	}
	if !hasDemoCmd() {
		t.Error("ListTrayCommands after install 应恢复含 demo/run")
	}

	// 逻辑卸载：MarkAbsent 后重新投影为 absent。
	if err := receipts.MarkAbsent("demo"); err != nil {
		t.Fatal(err)
	}
	eq(t, "demo.delivery", stateFor(t, reg, "demo").Delivery, DeliveryAbsent)
	if err := receipts.MarkInstalled("demo", ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}

	// 停用 → disabled/enable。
	if err := reg.SetEnabled("demo", false); err != nil {
		t.Fatal(err)
	}
	s = stateFor(t, reg, "demo")
	eq(t, "demo.policy", s.Policy, PolicyDisabled)
	eq(t, "demo.action", s.PrimaryAction, ActionEnable)
	eq(t, "demo.summary", s.Summary, SummaryInstalledIdle)
	eq(t, "aaa.policy(不受影响)", stateFor(t, reg, "aaa").Policy, PolicyEnabled)

	// 重新启用并激活 → active/open。
	if err := reg.SetEnabled("demo", true); err != nil {
		t.Fatal(err)
	}
	if err := reg.EnsureActive("demo"); err != nil {
		t.Fatal(err)
	}
	s = stateFor(t, reg, "demo")
	eq(t, "demo.runtime", s.Runtime, RuntimeActive)
	eq(t, "demo.action", s.PrimaryAction, ActionOpen)
	eq(t, "demo.summary", s.Summary, SummaryRunning)

	// 安全阻止 → blocked/none + 原因。
	reg.SetBlocked("demo", true)
	s = stateFor(t, reg, "demo")
	eq(t, "demo.policy", s.Policy, PolicyBlocked)
	eq(t, "demo.action", s.PrimaryAction, ActionNone)
	if s.Reason == "" {
		t.Error("blocked 投影必须携带非空 Reason")
	}
	eq(t, "demo.summary", s.Summary, SummaryBlocked)

	// Core 标记 → 策略维度投影为 mandatory。
	reg.SetBlocked("demo", false)
	reg.SetMandatory("demo", true)
	s = stateFor(t, reg, "demo")
	eq(t, "demo.policy", s.Policy, PolicyMandatory)
	eq(t, "aaa.policy(仍普通)", stateFor(t, reg, "aaa").Policy, PolicyEnabled)

	// 解除 mandatory 恢复 enabled 口径。
	reg.SetMandatory("demo", false)
	eq(t, "demo.policy", stateFor(t, reg, "demo").Policy, PolicyEnabled)
}

func enumStringsOf(states []ModuleState) []string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, s.ModuleID)
	}
	return out
}

// ---------------------------------------------------------------------------
// Acquire 调用门与 lease
// ---------------------------------------------------------------------------

func TestAcquireGateAndLease(t *testing.T) {
	t.Run("unknown module 拒绝", func(t *testing.T) {
		reg := NewRegistry(nil)
		if _, _, err := reg.Acquire("ghost"); !errors.Is(err, ErrUnknownModule) {
			t.Fatalf("Acquire unknown error = %v, want ErrUnknownModule", err)
		}
	})

	t.Run("成功取得 lease 且 drain 等待 release", func(t *testing.T) {
		reg := NewRegistry(nil)
		module := newRegistryTestModule("demo")
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}

		wrapper, release, err := reg.Acquire("demo")
		if err != nil {
			t.Fatalf("Acquire error = %v", err)
		}
		if wrapper == nil || wrapper.Module.Info().ID != "demo" {
			t.Fatalf("Acquire wrapper = %v, want demo wrapper", wrapper)
		}
		// lease 持有期间：initialized + inFlight>0 → busy。
		s := stateFor(t, reg, "demo")
		eq(t, "runtime(持锁)", s.Runtime, RuntimeBusy)
		eq(t, "action(持锁)", s.PrimaryAction, ActionOpen)

		disableDone := make(chan error, 1)
		go func() { disableDone <- reg.SetEnabled("demo", false) }()
		waitRuntime(t, reg, "demo", RuntimeStopping, time.Second)

		select {
		case err := <-disableDone:
			t.Fatalf("disable finished while lease held: %v", err)
		default:
		}
		// 关门后新 Acquire 必须被拒且不再初始化。
		if _, _, err := reg.Acquire("demo"); !errors.Is(err, ErrModuleDisabled) {
			t.Fatalf("Acquire during drain error = %v, want ErrModuleDisabled", err)
		}

		release()
		release() // 幂等：重复调用安全
		select {
		case err := <-disableDone:
			if err != nil {
				t.Fatalf("disable error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("disable did not finish after release")
		}
		if got := module.destroyCnt.Load(); got != 1 {
			t.Fatalf("OnDestroy count = %d, want 1", got)
		}
		s = stateFor(t, reg, "demo")
		eq(t, "policy(收口)", s.Policy, PolicyDisabled)
		eq(t, "runtime(收口)", s.Runtime, RuntimeInactive)
		eq(t, "action(收口)", s.PrimaryAction, ActionEnable)
	})

	t.Run("未安装拒绝(ErrModuleNotInstalled)", func(t *testing.T) {
		reg := NewRegistry(nil)
		module := newRegistryTestModule("demo")
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}
		receipts := newTestReceipts() // demo 无凭据
		reg.SetReceiptStorage(receipts)

		if _, _, err := reg.Acquire("demo"); !errors.Is(err, ErrModuleNotInstalled) {
			t.Fatalf("Acquire not-installed error = %v, want ErrModuleNotInstalled", err)
		}
		if got := module.initCount.Load(); got != 0 {
			t.Fatalf("OnInit count = %d, want 0（调用门必须先于懒激活拒绝）", got)
		}
		// 补凭据后同一扇门应放行。
		if err := receipts.MarkInstalled("demo", ReceiptBuiltinLogical); err != nil {
			t.Fatal(err)
		}
		_, release, err := reg.Acquire("demo")
		if err != nil {
			t.Fatalf("Acquire after receipt error = %v", err)
		}
		release()
	})

	t.Run("停用拒绝(ErrModuleDisabled)", func(t *testing.T) {
		store := &registryTestStore{enabled: map[string]bool{"demo": false}}
		reg := NewRegistry(store)
		module := newRegistryTestModule("demo")
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reg.Acquire("demo"); !errors.Is(err, ErrModuleDisabled) {
			t.Fatalf("Acquire disabled error = %v, want ErrModuleDisabled", err)
		}
		if got := module.initCount.Load(); got != 0 {
			t.Fatalf("OnInit count = %d, want 0", got)
		}
	})

	t.Run("blocked 拒绝(ErrModuleDisabled)", func(t *testing.T) {
		reg := NewRegistry(nil)
		module := newRegistryTestModule("demo")
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}
		reg.SetBlocked("demo", true)
		if _, _, err := reg.Acquire("demo"); !errors.Is(err, ErrModuleDisabled) {
			t.Fatalf("Acquire blocked error = %v, want ErrModuleDisabled", err)
		}
		if got := module.initCount.Load(); got != 0 {
			t.Fatalf("OnInit count = %d, want 0", got)
		}
		reg.SetBlocked("demo", false)
		_, release, err := reg.Acquire("demo")
		if err != nil {
			t.Fatalf("Acquire after unblock error = %v", err)
		}
		release()
	})

	t.Run("release 幂等且双 lease 各自计量", func(t *testing.T) {
		reg := NewRegistry(nil)
		module := newRegistryTestModule("demo")
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}
		_, releaseA, err := reg.Acquire("demo")
		if err != nil {
			t.Fatal(err)
		}
		_, releaseB, err := reg.Acquire("demo")
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "runtime(双 lease)", stateFor(t, reg, "demo").Runtime, RuntimeBusy)

		releaseA()
		releaseA() // 二次调用必须为空操作
		// 仍有 releaseB 在飞 → 依旧 busy；若 releaseA 重复扣减会假性归零。
		eq(t, "runtime(仅A释放×2)", stateFor(t, reg, "demo").Runtime, RuntimeBusy)
		releaseB()
		eq(t, "runtime(全释放)", stateFor(t, reg, "demo").Runtime, RuntimeActive)

		// drain 不被负数计数卡住：立即完成。
		done := make(chan error, 1)
		go func() { done <- reg.SetEnabled("demo", false) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("disable stuck after idempotent releases")
		}
	})

	t.Run("OnInit 失败滞留 failed 重试清除", func(t *testing.T) {
		reg := NewRegistry(nil)
		module := newRegistryTestModule("demo")
		boom := errors.New("boom")
		module.initErr = boom
		if err := reg.Register(module); err != nil {
			t.Fatal(err)
		}

		_, _, err := reg.Acquire("demo")
		if !errors.Is(err, boom) {
			t.Fatalf("Acquire init-fail error = %v, want wrapped boom", err)
		}
		// 失败事实滞留：投影 runtime=failed、primaryAction=retry。
		s := stateFor(t, reg, "demo")
		eq(t, "runtime(滞留)", s.Runtime, RuntimeFailed)
		eq(t, "action(滞留)", s.PrimaryAction, ActionRetry)
		eq(t, "summary(滞留)", s.Summary, SummaryFaulted)

		// 再次失败仍滞留 failed，不清除事实。
		if _, _, err := reg.Acquire("demo"); err == nil {
			t.Fatal("second Acquire should still fail")
		}
		eq(t, "runtime(二次滞留)", stateFor(t, reg, "demo").Runtime, RuntimeFailed)

		// 修复后重试成功 → failed 清除，投影回到 active/open。
		module.initErr = nil
		_, release, err := reg.Acquire("demo")
		if err != nil {
			t.Fatalf("retry Acquire error = %v", err)
		}
		s = stateFor(t, reg, "demo")
		eq(t, "runtime(重试成功)", s.Runtime, RuntimeBusy) // 持 lease 中
		eq(t, "action(重试成功)", s.PrimaryAction, ActionOpen)
		release()
		eq(t, "runtime(释放后)", stateFor(t, reg, "demo").Runtime, RuntimeActive)
		eq(t, "OnInit 次数", module.initCount.Load(), int32(3))
	})
}

// ---------------------------------------------------------------------------
// OnLifecycle 多播钩子
// ---------------------------------------------------------------------------

func TestOnLifecycleHooks(t *testing.T) {
	reg := NewRegistry(nil)
	module := newRegistryTestModule("demo")
	if err := reg.Register(module); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var events []string
	record := func(hook string) func(string, bool) {
		return func(id string, enabled bool) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, fmt.Sprintf("%s:%s:%v", hook, id, enabled))
		}
	}
	reg.OnLifecycle(record("h1"))
	reg.OnLifecycle(record("h2"))
	// fire 不得持有 wrapper 锁/写锁：钩子内回读 Registry 必须安全且不阻塞。
	reg.OnLifecycle(func(id string, enabled bool) {
		got := reg.IsEnabled(id)
		n := len(reg.ListStates())
		mu.Lock()
		defer mu.Unlock()
		events = append(events, fmt.Sprintf("readback:%v:len%d", got, n))
	})

	// Register 不触发钩子。
	mu.Lock()
	fired := len(events)
	mu.Unlock()
	if fired != 0 {
		t.Fatalf("hooks fired on Register: %v", events)
	}

	setEnabled := func(enabled bool) {
		done := make(chan error, 1)
		go func() { done <- reg.SetEnabled("demo", enabled) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("SetEnabled(%v) error = %v", enabled, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("SetEnabled(%v) blocked: lifecycle hook likely holds wrapper lock", enabled)
		}
	}
	setEnabled(false)
	setEnabled(true)

	want := []string{
		"h1:demo:false", "h2:demo:false", "readback:false:len1",
		"h1:demo:true", "h2:demo:true", "readback:true:len1",
	}
	mu.Lock()
	got := slices.Clone(events)
	mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("hook events =\n%v\nwant\n%v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Core 守卫：mandatory 拒绝停用与卸载（ADR-0001 §1.4，防投影分叉）
// ---------------------------------------------------------------------------

func TestMandatoryCoreGuards(t *testing.T) {
	reg := NewRegistry(nil)
	core := newRegistryTestModule("core")
	if err := reg.Register(core); err != nil {
		t.Fatal(err)
	}
	reg.SetMandatory("core", true)

	if err := reg.SetEnabled("core", false); err == nil {
		t.Error("mandatory 模块 SetEnabled(false) 必须被拒绝")
	}
	if err := reg.Uninstall("core"); err == nil {
		t.Error("mandatory 模块 Uninstall 必须被拒绝")
	}
	if !reg.IsEnabled("core") {
		t.Error("被拒的停用不得改变启用态")
	}
	if got := stateFor(t, reg, "core"); got.Policy != PolicyMandatory || !got.Visible() {
		t.Errorf("投影 = %s/visible=%v, want mandatory 且可见", got.Policy, got.Visible())
	}

	// 解除标记后恢复正常生命周期。
	reg.SetMandatory("core", false)
	if err := reg.SetEnabled("core", false); err != nil {
		t.Fatalf("解除 mandatory 后停用应成功: %v", err)
	}
	if got := stateFor(t, reg, "core"); got.Policy != PolicyDisabled {
		t.Errorf("Policy = %s, want disabled", got.Policy)
	}
}

// TestSetHealthRemoteVersion 覆盖健康维度覆盖写入的版本账目：update-available
// 记录上游新版本并盖进投影（纯展示附加，不进状态机）；current/其他健康值
// 一律清空；投影仅在 health=update-available 时携带 RemoteVersion。
func TestSetHealthRemoteVersion(t *testing.T) {
	reg := NewRegistry(nil)
	demo := newRegistryTestModule("demo")
	if err := reg.Register(demo); err != nil {
		t.Fatal(err)
	}

	reg.SetHealth("demo", HealthUpdateAvailable, "2.3.4")
	got := stateFor(t, reg, "demo")
	eq(t, "demo.health", got.Health, HealthUpdateAvailable)
	eq(t, "demo.remoteVersion", got.RemoteVersion, "2.3.4")

	// 旧格式回灌等无版本场景：update-available 点亮但版本留空，不谎报。
	reg.SetHealth("demo", HealthUpdateAvailable, "")
	eq(t, "demo.remoteVersion（无版本回灌）", stateFor(t, reg, "demo").RemoteVersion, "")

	// 回到 current：即便误传版本也必须清空。
	reg.SetHealth("demo", HealthCurrent, "9.9.9")
	got = stateFor(t, reg, "demo")
	eq(t, "demo.health", got.Health, HealthCurrent)
	eq(t, "demo.remoteVersion（current 清空）", got.RemoteVersion, "")

	// 其他健康值（撤回类）同样不带版本。
	reg.SetHealth("demo", HealthUpdateAvailable, "2.3.4")
	reg.SetHealth("demo", HealthRevoked, "")
	got = stateFor(t, reg, "demo")
	eq(t, "demo.health", got.Health, HealthRevoked)
	eq(t, "demo.remoteVersion（revoked 清空）", got.RemoteVersion, "")

	// 空串恢复 current 缺省：版本一并收口。
	reg.SetHealth("demo", HealthUpdateAvailable, "2.3.4")
	reg.SetHealth("demo", "", "")
	eq(t, "demo.remoteVersion（空串恢复 current）", stateFor(t, reg, "demo").RemoteVersion, "")
}
