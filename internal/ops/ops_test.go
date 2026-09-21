package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/packages/go/operation"
)

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
