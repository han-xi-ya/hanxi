package updatewatch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
)

// ---------- 测试替身：Module + UpdateChecker 二合一 ----------

// fakeModule 同时充当注册表里的占位模块与可编程检查器；调用计数与并发水位
// 用于断言调度纪律（并发上限、single-flight 合并、逐模块一轮至多一次）。
type fakeModule struct {
	id string

	fn func(ctx context.Context) (string, string, bool, error)

	mu     sync.Mutex
	calls  int
	inFly  int
	maxFly int
}

func (f *fakeModule) Info() extapi.ModuleInfo      { return extapi.ModuleInfo{ID: f.id} }
func (f *fakeModule) Nav() []extapi.NavEntry       { return nil }
func (f *fakeModule) Services() []extapi.Service   { return nil }
func (f *fakeModule) OnInit(context.Context) error { return nil }
func (f *fakeModule) OnDestroy() error             { return nil }
func (f *fakeModule) IsInitialized() bool          { return true }

func (f *fakeModule) CheckUpdate(ctx context.Context) (string, string, bool, error) {
	f.mu.Lock()
	f.calls++
	f.inFly++
	if f.inFly > f.maxFly {
		f.maxFly = f.inFly
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFly--
		f.mu.Unlock()
	}()
	return f.fn(ctx)
}

func (f *fakeModule) snapshot() (calls, maxFly int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.maxFly
}

// newSched 组装带占位模块的调度器；返回模块 ID 顺序（注册表按 ID 排序）。
func newSched(t *testing.T, stateDir string, fakes ...*fakeModule) (*Scheduler, *extapi.Registry, map[string]*fakeModule) {
	t.Helper()
	registry := extapi.NewRegistry(nil)
	mods := make([]extapi.Module, 0, len(fakes))
	checkers := map[string]extapi.UpdateChecker{}
	byID := map[string]*fakeModule{}
	for _, f := range fakes {
		mods = append(mods, f)
		checkers[f.id] = f
		byID[f.id] = f
	}
	if err := registry.Register(mods...); err != nil {
		t.Fatalf("register: %v", err)
	}
	return New(registry, checkers, stateDir), registry, byID
}

func healthOf(t *testing.T, registry *extapi.Registry, id string) extapi.HealthState {
	t.Helper()
	for _, st := range registry.ListStates() {
		if st.ModuleID == id {
			return st.Health
		}
	}
	t.Fatalf("模块 %q 不在状态投影中", id)
	return ""
}

// remoteVersionOf 取状态投影中展示的远程新版本号（health≠update-available
// 或无版本记录时为空串）。
func remoteVersionOf(t *testing.T, registry *extapi.Registry, id string) string {
	t.Helper()
	for _, st := range registry.ListStates() {
		if st.ModuleID == id {
			return st.RemoteVersion
		}
	}
	t.Fatalf("模块 %q 不在状态投影中", id)
	return ""
}

func staticResult(local, remote string, up bool, err error) func(context.Context) (string, string, bool, error) {
	return func(context.Context) (string, string, bool, error) {
		return local, remote, up, err
	}
}

// TestSweepHealthOutcomes 成功判定逐模块写健康值；失败模块保持原健康值
// （不谎报"无更新"、也不清除既有信号），checked 只计成功数。
func TestSweepHealthOutcomes(t *testing.T) {
	up := &fakeModule{id: "alpha", fn: staticResult("1.0.0", "1.1.0", true, nil)}
	cur := &fakeModule{id: "beta", fn: staticResult("2.0.0", "2.0.0", false, nil)}
	fail := &fakeModule{id: "gamma", fn: staticResult("", "", false, os.ErrDeadlineExceeded)}

	sched, registry, _ := newSched(t, t.TempDir(), up, cur, fail)
	// gamma 预置 update-available（含展示版本）：检查失败后健康值与版本都必须原样保留。
	registry.SetHealth("gamma", extapi.HealthUpdateAvailable, "3.0.0")

	checked, err := sched.CheckNow(context.Background())
	if err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if checked != 2 {
		t.Errorf("checked = %d, want 2（失败模块不计入）", checked)
	}
	if got := healthOf(t, registry, "alpha"); got != extapi.HealthUpdateAvailable {
		t.Errorf("alpha health = %q, want update-available", got)
	}
	if got := healthOf(t, registry, "beta"); got != extapi.HealthCurrent {
		t.Errorf("beta health = %q, want current", got)
	}
	if got := healthOf(t, registry, "gamma"); got != extapi.HealthUpdateAvailable {
		t.Errorf("gamma 检查失败后 health = %q, want 保持 update-available", got)
	}
	// 版本贯穿断言：成功判定的 remote 经 SetHealth 落到 ListStates 投影；
	// current 判定不带版本；失败模块既有版本不被清除。
	if got := remoteVersionOf(t, registry, "alpha"); got != "1.1.0" {
		t.Errorf("alpha remoteVersion = %q, want 1.1.0", got)
	}
	if got := remoteVersionOf(t, registry, "beta"); got != "" {
		t.Errorf("beta remoteVersion = %q, want 空（current 不带版本）", got)
	}
	if got := remoteVersionOf(t, registry, "gamma"); got != "3.0.0" {
		t.Errorf("gamma 检查失败后 remoteVersion = %q, want 保持 3.0.0", got)
	}
}

// TestConcurrencyBound 并发上限 ≤3：8 个慢检查器的同时在飞水位不得越界。
func TestConcurrencyBound(t *testing.T) {
	const n = 8
	fakes := make([]*fakeModule, 0, n)
	for i := 0; i < n; i++ {
		fakes = append(fakes, &fakeModule{id: string(rune('a' + i)), fn: func(ctx context.Context) (string, string, bool, error) {
			time.Sleep(30 * time.Millisecond)
			return "1", "1", false, nil
		}})
	}
	sched, _, _ := newSched(t, "", fakes...)
	checked, err := sched.CheckNow(context.Background())
	if err != nil || checked != n {
		t.Fatalf("CheckNow = (%d, %v)", checked, err)
	}
	maxFly := 0
	for _, f := range fakes {
		calls, fly := f.snapshot()
		if calls != 1 {
			t.Errorf("%s 被调用 %d 次, want 1", f.id, calls)
		}
		if fly > maxFly {
			maxFly = fly
		}
	}
	if maxFly > 3 {
		t.Errorf("并发水位 %d 超过上限 3", maxFly)
	}
}

// TestSingleFlightJoin 同轮 in-flight 时并发 CheckNow 合并：检查器一轮只被
// 调一次，join 者与发起者拿到同一计数。
func TestSingleFlightJoin(t *testing.T) {
	block := make(chan struct{})
	fake := &fakeModule{id: "slow", fn: func(ctx context.Context) (string, string, bool, error) {
		<-block
		return "1", "2", true, nil
	}}
	sched, registry, _ := newSched(t, "", fake)

	type res struct {
		n   int
		err error
	}
	results := make(chan res, 2)
	go func() { n, err := sched.CheckNow(context.Background()); results <- res{n, err} }()
	time.Sleep(20 * time.Millisecond) // 让第一轮进入 in-flight
	go func() { n, err := sched.CheckNow(context.Background()); results <- res{n, err} }()
	time.Sleep(20 * time.Millisecond)
	close(block)

	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil || r.n != 1 {
			t.Fatalf("join/发起者结果 = (%d, %v), want (1, nil)", r.n, r.err)
		}
	}
	if calls, _ := fake.snapshot(); calls != 1 {
		t.Errorf("检查器被调用 %d 次, want 1（合并轮次）", calls)
	}
	if got := healthOf(t, registry, "slow"); got != extapi.HealthUpdateAvailable {
		t.Errorf("health = %q, want update-available", got)
	}
}

// TestPerCheckTimeout 单模块超时按失败处理：保持原健康值、不计入 checked。
func TestPerCheckTimeout(t *testing.T) {
	hang := &fakeModule{id: "hang", fn: func(ctx context.Context) (string, string, bool, error) {
		<-ctx.Done() // 模拟无视取消的远程链：靠调度器超时收口
		return "", "", false, ctx.Err()
	}}
	ok := &fakeModule{id: "ok", fn: staticResult("", "", false, nil)}
	sched, registry, _ := newSched(t, "", hang, ok)
	sched.checkBudget = 30 * time.Millisecond
	sched.concurrency = 2

	checked, err := sched.CheckNow(context.Background())
	if err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if checked != 1 {
		t.Errorf("checked = %d, want 1（超时模块不计）", checked)
	}
	if got := healthOf(t, registry, "hang"); got != extapi.HealthCurrent {
		t.Errorf("超时模块 health = %q, want 保持缺省 current", got)
	}
}

// TestCacheMergeAndRestore 结果落盘 state/updates.json：仅记 update-available
// 集合；失败模块保留历史条目；新调度器 Restore 回灌首帧。
func TestCacheMergeAndRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")

	upA := &fakeModule{id: "a-up", fn: staticResult("1", "2", true, nil)}
	upB := &fakeModule{id: "b-up", fn: staticResult("1", "2", true, nil)}
	fail := &fakeModule{id: "c-fail", fn: staticResult("", "", false, os.ErrDeadlineExceeded)}
	sched, _, _ := newSched(t, dir, upA, upB, fail)
	if _, err := sched.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	var snap snapshot
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读缓存: %v", err)
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("解析缓存: %v", err)
	}
	if want := map[string]string{"a-up": "2", "b-up": "2"}; !reflect.DeepEqual(snap.Available, want) {
		t.Errorf("Available = %v, want %v", snap.Available, want)
	}

	// 第二轮：a-up 变 current（条目移除），b-up 保持，c-fail 不在本轮结果——
	// 合并语义下 c-fail 从未成功判定，本就不该出现在快照里。
	upA.fn = staticResult("2", "2", false, nil)
	if _, err := sched.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, err = os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	snap = snapshot{}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"b-up": "2"}; !reflect.DeepEqual(snap.Available, want) {
		t.Errorf("二轮后 Available = %v, want %v", snap.Available, want)
	}

	// 全新调度器 + 空 registry：Restore 回灌 b-up（版本号一并回灌，供首帧投影展示）。
	sched2, registry2, _ := newSched(t, dir, upA, upB, fail)
	if n := sched2.Restore(); n != 1 {
		t.Fatalf("Restore = %d, want 1", n)
	}
	if got := healthOf(t, registry2, "b-up"); got != extapi.HealthUpdateAvailable {
		t.Errorf("回灌后 b-up health = %q", got)
	}
	if got := remoteVersionOf(t, registry2, "b-up"); got != "2" {
		t.Errorf("回灌后 b-up remoteVersion = %q, want 2", got)
	}
	if got := healthOf(t, registry2, "a-up"); got != extapi.HealthCurrent {
		t.Errorf("回灌不应越界点亮 a-up：%q", got)
	}
}

// TestCacheLegacyFormatMigration 旧落盘形状（available 为 []string、无版本号）
// 容忍迁移：Restore 仍按 update-available 点亮（版本留空不谎报），merge 一轮后
// 自动升级为新 map 形状。
func TestCacheLegacyFormatMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")
	legacy := `{"checkedAt":"2026-01-01T00:00:00Z","available":["old-a","old-b"]}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	upA := &fakeModule{id: "old-a", fn: staticResult("1", "1", false, nil)}
	upB := &fakeModule{id: "old-b", fn: staticResult("1", "2", true, nil)}
	sched, registry, _ := newSched(t, dir, upA, upB)
	if n := sched.Restore(); n != 2 {
		t.Fatalf("旧格式 Restore = %d, want 2", n)
	}
	for _, id := range []string{"old-a", "old-b"} {
		if got := healthOf(t, registry, id); got != extapi.HealthUpdateAvailable {
			t.Errorf("旧格式回灌 %s health = %q, want update-available", id, got)
		}
		if got := remoteVersionOf(t, registry, id); got != "" {
			t.Errorf("旧格式无版本记录，%s remoteVersion = %q, want 空（不谎报）", id, got)
		}
	}

	// 一轮成功判定后落盘升级为新形状：old-a 转 current 移除，old-b 带版本保留。
	if _, err := sched.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("迁移后缓存应可按新形状解析: %v", err)
	}
	if want := map[string]string{"old-b": "2"}; !reflect.DeepEqual(snap.Available, want) {
		t.Errorf("迁移后 Available = %v, want %v", snap.Available, want)
	}
}

// ---------- 幽灵状态（卸载后三账联动）回归 ----------

// fakeReceipts 内存版 ReceiptStorage：驱动 Registry 安装事实裁决，测试可
// 在感知轮次在飞期间翻转凭据，复现"卸载发生在判定与收口之间"的真实竞态。
type fakeReceipts struct {
	mu        sync.Mutex
	installed map[string]bool
}

func newFakeReceipts(ids ...string) *fakeReceipts {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return &fakeReceipts{installed: set}
}

func (f *fakeReceipts) IsInstalled(moduleID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installed[moduleID]
}

func (f *fakeReceipts) MarkInstalled(moduleID string, _ extapi.ReceiptKind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed[moduleID] = true
	return nil
}

func (f *fakeReceipts) MarkAbsent(moduleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.installed, moduleID)
	return nil
}

func (f *fakeReceipts) Installed() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]bool, len(f.installed))
	for id, ok := range f.installed {
		out[id] = ok
	}
	return out
}

// summaryOfState 取指定模块投影的摘要键。
func summaryOfState(t *testing.T, registry *extapi.Registry, id string) extapi.SummaryKey {
	t.Helper()
	for _, st := range registry.ListStates() {
		if st.ModuleID == id {
			return st.Summary
		}
	}
	t.Fatalf("模块 %q 不在状态投影中", id)
	return ""
}

// loadSnapshotFor 读取 state/updates.json 落盘形状（断言剔账结果）。
func loadSnapshotFor(t *testing.T, path string) snapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读缓存: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("解析缓存: %v", err)
	}
	return snap
}

// TestUninstallDuringRoundInvalidatesEntry 感知在飞时卸载（三态之一）：
// 检查器已产出"有更新"结论后才发生卸载——收口写账前复查安装事实，该模块
// 不写健康覆盖、不计 checked，磁盘旧账（上轮真实点亮过）被 merge 全量复查
// 剔除；仍在册在装模块正常成账。
func TestUninstallDuringRoundInvalidatesEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")
	if err := os.WriteFile(path, []byte(`{"checkedAt":"2026-01-01T00:00:00+08:00","available":{"gone":"2.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	flip := make(chan struct{})
	gone := &fakeModule{id: "gone", fn: func(context.Context) (string, string, bool, error) {
		close(started)
		<-flip // 等测试翻转 receipt（卸载发生在判定与写账之间）后再交回结论
		return "1.0", "2.0", true, nil
	}}
	keep := &fakeModule{id: "keep", fn: staticResult("1.0", "2.0", true, nil)}

	sched, registry, _ := newSched(t, dir, gone, keep)
	receipts := newFakeReceipts("gone", "keep")
	registry.SetReceiptStorage(receipts)

	round := make(chan int, 1)
	go func() {
		n, err := sched.CheckNow(context.Background())
		if err != nil {
			t.Errorf("CheckNow: %v", err)
		}
		round <- n
	}()
	<-started
	if err := receipts.MarkAbsent("gone"); err != nil {
		t.Fatal(err)
	}
	close(flip)
	if n := <-round; n != 1 {
		t.Errorf("checked = %d, want 1（在飞期间卸载的模块不计入）", n)
	}

	if got := healthOf(t, registry, "gone"); got != extapi.HealthCurrent {
		t.Errorf("卸载竞态后 gone health = %q, want current（幽灵账不写健康覆盖）", got)
	}
	if got := remoteVersionOf(t, registry, "gone"); got != "" {
		t.Errorf("gone remoteVersion = %q, want 空", got)
	}
	if got := summaryOfState(t, registry, "gone"); got != extapi.SummaryNotInstalled {
		t.Errorf("gone summary = %q, want not-installed（卸载后的诚实呈现，不谎报 running/update）", got)
	}
	if got := healthOf(t, registry, "keep"); got != extapi.HealthUpdateAvailable {
		t.Errorf("keep health = %q, want update-available（在册模块不受波及）", got)
	}
	if want := map[string]string{"keep": "2.0"}; !reflect.DeepEqual(loadSnapshotFor(t, path).Available, want) {
		t.Errorf("磁盘旧账未剔净: Available = %v, want %v", loadSnapshotFor(t, path).Available, want)
	}
}

// TestRestorePrunesStaleEntries 缓存旧账启动自愈（三态之二）：updates.json
// 存在条目但模块已卸载 / ID 根本不在册 → 回灌时主动剔账并回写磁盘，
// CheckedAt 保持原值（剔账不是感知，不伪造"刚查过"）；在册在装条目照常回灌。
func TestRestorePrunesStaleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")
	const checkedAt = "2026-01-01T00:00:00+08:00"
	pre := `{"checkedAt":"` + checkedAt + `","available":{"live":"2.0","absent-mod":"3.0","ghost-mod":""}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	live := &fakeModule{id: "live", fn: staticResult("1.0", "2.0", true, nil)}
	absent := &fakeModule{id: "absent-mod", fn: staticResult("1.0", "3.0", true, nil)}
	sched, registry, _ := newSched(t, dir, live, absent)
	registry.SetReceiptStorage(newFakeReceipts("live")) // absent-mod 已卸载；ghost-mod 不在册

	if applied := sched.Restore(); applied != 1 {
		t.Fatalf("Restore applied = %d, want 1（旧账两条应被剔除）", applied)
	}
	if got := healthOf(t, registry, "live"); got != extapi.HealthUpdateAvailable {
		t.Errorf("live health = %q, want update-available", got)
	}
	if got := healthOf(t, registry, "absent-mod"); got != extapi.HealthCurrent {
		t.Errorf("absent-mod health = %q, want current（不得回灌幽灵账）", got)
	}
	snap := loadSnapshotFor(t, path)
	if want := map[string]string{"live": "2.0"}; !reflect.DeepEqual(snap.Available, want) {
		t.Errorf("剔账后磁盘 Available = %v, want %v", snap.Available, want)
	}
	if snap.CheckedAt != checkedAt {
		t.Errorf("剔账回写不得伪造 CheckedAt: got %q, want %q", snap.CheckedAt, checkedAt)
	}
}

// TestResidualAssetsNeverLightPhantom 卸载后残留资产/外部进程真在跑的诚实
// 呈现（三态之三）：模块无 receipt，但其版本目录与进程客观存在，检查器如实
// 报"有更新"——感知链仍不为其写健康账（本机安装事实不存在，更新无从谈起），
// 投影恒 not-installed：既不谎报 running，也不谎报 update-available。
func TestResidualAssetsNeverLightPhantom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")

	res := &fakeModule{id: "res", fn: staticResult("1.0", "2.0", true, nil)} // 残留资产可被扫到
	norm := &fakeModule{id: "norm", fn: staticResult("1.0", "2.0", true, nil)}
	sched, registry, _ := newSched(t, dir, res, norm)
	registry.SetReceiptStorage(newFakeReceipts("norm")) // res 已卸载（版本目录/进程残留）

	checked, err := sched.CheckNow(context.Background())
	if err != nil || checked != 1 {
		t.Fatalf("CheckNow = (%d, %v), want (1, nil)", checked, err)
	}
	if got := healthOf(t, registry, "res"); got != extapi.HealthCurrent {
		t.Errorf("残留模块 health = %q, want current（不写幽灵账）", got)
	}
	if got := summaryOfState(t, registry, "res"); got != extapi.SummaryNotInstalled {
		t.Errorf("残留模块 summary = %q, want not-installed", got)
	}
	snap := loadSnapshotFor(t, path)
	if want := map[string]string{"norm": "2.0"}; !reflect.DeepEqual(snap.Available, want) {
		t.Errorf("残留模块不得进盘账: Available = %v, want %v", snap.Available, want)
	}
}

// TestStopCancelsStartupRound Start 的延迟首检在 Stop 后不再执行。
func TestStopCancelsStartupRound(t *testing.T) {
	fake := &fakeModule{id: "x", fn: staticResult("", "", false, nil)}
	sched, _, _ := newSched(t, "", fake)
	sched.startDelay = 10 * time.Millisecond
	sched.Start()
	sched.Stop()
	time.Sleep(80 * time.Millisecond)
	if calls, _ := fake.snapshot(); calls != 0 {
		t.Errorf("Stop 后延迟首检仍执行了 %d 次", calls)
	}
}
