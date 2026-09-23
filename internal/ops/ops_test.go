package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/operation"
)

func TestBeginTxnWithLifecycleTerminalReleasesLease(t *testing.T) {
	SetKernel(nil, nil)
	defer SetKernel(nil, nil)

	holder := extapi.NewLeaseHolder("demo")
	lease, err := holder.EnterBackground(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	txn, err := BeginTxnWithLifecycle(context.Background(), lease, "demo", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "1.0", "txn-release", []string{"download"})
	if err != nil {
		t.Fatal(err)
	}
	if err := txn.Step(0); err != nil {
		t.Fatalf("健康步进失败: %v", err)
	}
	if err := txn.Done(); err != nil {
		t.Fatalf("Done: %v", err)
	}
	// 终态后 lease 归事务释放：context 已取消、重复收口安全（once 双保险）
	if err := txn.Context().Err(); err == nil {
		t.Fatal("Done 后事务 ctx 应已取消")
	}
	lease.Release() // 再释放一次不得 panic、不得双重扣减
	txn.Fail("late", "迟到的失败收口")
	if err := txn.Step(0); err == nil {
		t.Fatal("收口后 Step 必须被拒")
	}
}

// freshStore 打开真实 journal Store（临时目录），返回 (store, dir)。
func freshStore(t *testing.T) (*operation.Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "journals")
	store, err := operation.OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store, dir
}

func TestBeginTxnWithLifecyclePreservesAndCancelsContext(t *testing.T) {
	SetKernel(nil, nil)
	defer SetKernel(nil, nil)

	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	txn, err := BeginTxnWithLifecycle(parent, nil, "demo", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "1.0", "txn-lifecycle", []string{"download"})
	if err != nil {
		t.Fatal(err)
	}
	if err := txn.Context().Err(); err != nil {
		t.Fatalf("transaction context unexpectedly canceled: %v", err)
	}
	parentCancel()
	if err := txn.Context().Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation not propagated: %v", err)
	}
	if err := txn.Step(0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Step after cancellation = %v, want context.Canceled", err)
	}
	txn.Fail("operation-cancelled", "test canceled")
}
func TestBeginTxnNoKernelDesignMode(t *testing.T) {
	// SetKernel 从未调用过的"无账设计模式"（单测/头less）：BeginTxn 返回内存态 Txn。
	// 注意本测试对全局 kernelSet 有顺序依赖：必须最先注册且其它测试自行 SetKernel。
	if kernelLoaded() {
		t.Skip("前序测试已注入内核，无账设计模式仅在全新进程可验证")
	}
	txn, err := BeginTxn("m", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "1.0", "txn-design", []string{"download"})
	if err != nil || txn == nil {
		t.Fatalf("设计模式应放行内存态事务: %v", err)
	}
	if err := txn.Step(0); err != nil {
		t.Errorf("无账模式 Step 必须 no-op 成功: %v", err)
	}
}

func TestStepFailureDegradesAndRefusesNewTxn(t *testing.T) {
	store, dir := freshStore(t)
	t.Cleanup(func() { SetKernel(nil, nil) })
	SetKernel(store, nil)

	txn, err := BeginTxn("markeron", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "9.9.9", "txn-step-fail", []string{"download", "install"})
	if err != nil {
		t.Fatal(err)
	}
	// 健康步进：落盘成功。
	if err := txn.Step(0); err != nil {
		t.Fatalf("健康步进失败: %v", err)
	}
	// 抽掉账本目录（模拟磁盘故障/目录被清）→ 步进必须报错并全场降级。
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := txn.Step(1); err == nil {
		t.Fatal("账本不可写时 Step 必须返回错误（fail-closed）")
	}
	if JournalDegraded() == nil {
		t.Fatal("步进失败必须挂全场降级标记")
	}
	// 后续新事务一律拒绝——"无账执行"被焊死。
	if _, err := BeginTxn("markeron", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "9.9.10", "txn-blocked", []string{"download"}); err == nil {
		t.Fatal("降级后 BeginTxn 必须拒绝新托管写事务")
	} else if !strings.Contains(err.Error(), "账本") {
		t.Errorf("拒绝原因必须面向用户可诊断: %v", err)
	}
	// 已降级事务的后续 Done/Fail 静默（幂等闩，不得二次收口覆盖 journal-degraded 终态）。
	txn.Done()
	txn.Fail("late", "迟到收口")
}

func TestAssembleTimeDegradationAndReset(t *testing.T) {
	t.Cleanup(func() { SetKernel(nil, nil) })
	SetKernel(nil, nil) // 已注入但无 store/hub：无账模式
	if _, err := BeginTxn("m", extapi.OpInstall, extapi.DeliveryBuiltinLogical, "1", "txn-mem", []string{"a"}); err != nil {
		t.Fatal("无账模式不应被拒:", err)
	}
	MarkJournalDegraded(errors.New("装配期账本打开失败"))
	if _, err := BeginTxn("m", extapi.OpInstall, extapi.DeliveryBuiltinLogical, "1", "txn-refuse", []string{"a"}); err == nil {
		t.Fatal("装配降级必须拒绝新事务")
	}
	SetKernel(nil, nil) // 重建内核 = 明示账本已恢复
	if JournalDegraded() != nil {
		t.Fatal("SetKernel 应复位降级标记")
	}
}

// ---------- 2b-4：下载中停用/退出全链集成（真 Registry + 真 journal） ----------

// lifecycleModule 最小模块替身：ops 集成测试需要可被 Acquire/停用的真注册表模块。
type lifecycleModule struct {
	id     string
	inited bool
}

func (m *lifecycleModule) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: m.id, Name: m.id}
}
func (m *lifecycleModule) Nav() []extapi.NavEntry       { return nil }
func (m *lifecycleModule) Services() []extapi.Service   { return nil }
func (m *lifecycleModule) OnInit(context.Context) error { m.inited = true; return nil }
func (m *lifecycleModule) OnDestroy() error             { m.inited = false; return nil }
func (m *lifecycleModule) IsInitialized() bool          { return m.inited }

// TestDeactivateCancelsInFlightTxn 模拟机主口述场景「下载一半停用模块」：
// 后台租约事务步进（journal 落 running）→ Registry 停用 drain 先 Cancel →
// worker 感知 ctx 取消、按 operation-cancelled 收口落账、Release 后 drain 放行；
// journal 终态必须 failed/operation-cancelled，不得出现 succeeded 假账。
func TestDeactivateCancelsInFlightTxn(t *testing.T) {
	store, _ := freshStore(t)
	SetKernel(store, operation.NewHub(store))
	t.Cleanup(func() { SetKernel(nil, nil) })

	reg := extapi.NewRegistry(nil)
	if err := reg.Register(&lifecycleModule{id: "dl-sim"}); err != nil {
		t.Fatal(err)
	}
	holder := extapi.NewLeaseHolder("dl-sim")
	holder.SetGate(reg.Gate())

	lease, err := holder.EnterBackground(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	txn, err := BeginTxnWithLifecycle(context.Background(), lease, "dl-sim", extapi.OpInstall,
		extapi.DeliveryManagedDeclarative, "9.9.9", "txn-it-cancel", []string{"download", "unpack"})
	if err != nil {
		t.Fatal(err)
	}

	workerDone := make(chan error, 1)
	go func() { // 模拟 service worker：步进落账后等待取消，按 2b 纪律收口
		if err := txn.Step(0); err != nil {
			workerDone <- err
			return
		}
		<-txn.Context().Done()
		if txn.JournalFailed() {
			txn.Fail("journal-degraded", "journal 步进失败")
		} else if txn.Err() != nil {
			txn.Fail("operation-cancelled", "托管操作已取消")
		}
		txn.Close()
		workerDone <- nil
	}()

	// 等 journal 落 download/running 后再停用（确保测的是「在途中断」）
	deadline := time.Now().Add(2 * time.Second)
	for {
		j, gerr := store.Get("txn-it-cancel")
		if gerr == nil && j.Phase == "download" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("journal 步进未落账: %+v err %v", j, gerr)
		}
		time.Sleep(5 * time.Millisecond)
	}

	disableDone := make(chan error, 1)
	go func() { disableDone <- reg.SetEnabled("dl-sim", false) }()

	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker 收口异常: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("停用未取消在途事务（drain 未收到取消信号或 worker 未收口）")
	}
	select {
	case err := <-disableDone:
		if err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker 收口后 drain 未放行（lease 释放链断裂）")
	}

	j, err := store.Get("txn-it-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed {
		t.Fatalf("journal 终态应为 failed，实际 %s", j.State)
	}
	if j.Error == nil || j.Error.Code != "operation-cancelled" {
		t.Fatalf("终态错误码应为 operation-cancelled，实际 %+v", j.Error)
	}
	if txn.Done() == nil {
		t.Fatal("已收口事务的 Done 必须返回非 nil（不得补记成功）")
	}
}
