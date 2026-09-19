// operations_service_test.go 覆盖 Wave 4-B 操作观察面 RPC：ListOperations 的
// Active+Recent+resumable 合并形状、DismissResumable 的背书清理与收口语义
// （含对已成功账本拒绝翻案、未知事务报错、重复忽略幂等）。
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	markeronversion "hanxi/internal/modules/markeron/version"
	rufusversion "hanxi/internal/modules/rufus/version"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// newOpService 组装带观察面的 AppService（真 tmp 账本 + 双样本版本树登记）。
func newOpService(t *testing.T) (*AppService, *operation.Store, string) {
	t.Helper()
	store, err := operation.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	versionsDir := t.TempDir()
	trees := map[string]*artifact.Tree{
		"markeron": markeronversion.OpenTree(versionsDir),
		"rufus":    rufusversion.OpenTree(versionsDir),
	}
	hub := operation.NewHub(store)
	svc, _, _ := newTestAppService(t, "markeron", "rufus")
	svc.SetOperations(hub, makeDismissResumable(store, hub, trees))
	return svc, store, versionsDir
}

// beginCrashedTxn 落一笔"崩溃遗留"的未收口事务账 + 同名 staging 半件。
func beginCrashedTxn(t *testing.T, store *operation.Store, versionsDir, module, txnID, entry string) string {
	t.Helper()
	if err := store.Begin(operation.Journal{
		TransactionID: txnID,
		Operation:     operation.TxnOpInstall,
		DeliveryKind:  extapi.DeliveryManagedDeclarative,
		ModuleID:      module,
		Phase:         "download",
		Steps:         []operation.Step{{Name: "download", State: operation.TxnRunning}},
	}); err != nil {
		t.Fatal(err)
	}
	tree := markeronversion.OpenTree(versionsDir)
	if module == "rufus" {
		tree = rufusversion.OpenTree(versionsDir)
	}
	staging, discard, err := tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟崩溃：故意不丢弃，staging 半件留在盘上
	if err := os.WriteFile(filepath.Join(staging, entry), []byte("half"), 0644); err != nil {
		t.Fatal(err)
	}
	return staging
}

func TestListOperationsShape(t *testing.T) {
	svc, store, versionsDir := newOpService(t)

	// resumable 回灌：先落账再重建 Hub（NewHub 启动期回灌 Pending）
	beginCrashedTxn(t, store, versionsDir, "markeron", "txn-resumable", "MarkerOn.exe")
	beginCrashedTxn(t, store, versionsDir, "rufus", "txn-resumable-2", "rufus.exe")
	hub := operation.NewHub(store)
	trees := map[string]*artifact.Tree{
		"markeron": markeronversion.OpenTree(versionsDir),
		"rufus":    rufusversion.OpenTree(versionsDir),
	}
	svc.SetOperations(hub, makeDismissResumable(store, hub, trees))

	// 一笔在途 + 一笔已收口终态
	live := hub.Begin("markeron", extapi.OpUpdate, "")
	live.Phase("download")
	ended := hub.Begin("rufus", extapi.OpInstall, "")
	ended.Done()

	got := svc.ListOperations()
	byID := map[string]extapi.Operation{}
	for _, op := range got {
		if _, dup := byID[op.ID]; dup {
			t.Fatalf("ListOperations 出现重复 ID %s", op.ID)
		}
		byID[op.ID] = op
	}
	// 形状：Active 在最前（登记序），随后 Recent(30)（含 resumable）补集
	if len(got) < 4 {
		t.Fatalf("合并结果过短: %+v", got)
	}
	if got[0].ID != live.Snapshot().ID || got[0].Status != extapi.OpRunning {
		t.Errorf("首位应为在途记录: %+v", got[0])
	}
	if byID["resumed-txn-resumable"].Error == nil || byID["resumed-txn-resumable"].Error.Code != "resumable" {
		t.Errorf("resumable 回灌缺失: %+v", got)
	}
	if byID["resumed-txn-resumable-2"].ModuleID != "rufus" {
		t.Errorf("双样本 resumable 应各自归属: %+v", got)
	}
}

func TestDismissResumableCleansEndorsedStaging(t *testing.T) {
	svc, store, versionsDir := newOpService(t)
	staging := beginCrashedTxn(t, store, versionsDir, "rufus", "txn-dismiss", "rufus.exe")
	// 重演启动序列：账本落账在观察面构造之前（NewHub 回灌反映现态）
	hub := operation.NewHub(store)
	trees := map[string]*artifact.Tree{
		"markeron": markeronversion.OpenTree(versionsDir),
		"rufus":    rufusversion.OpenTree(versionsDir),
	}
	svc.SetOperations(hub, makeDismissResumable(store, hub, trees))
	if _, present := findOp(svc.ListOperations(), "resumed-txn-dismiss"); !present {
		t.Fatal("前置条件：回灌应可见 resumable 记录")
	}

	// 别的模块/别的事务的残骸不得被误伤（共享 versions 根）
	other := markeronversion.OpenTree(versionsDir)
	otherStaging, otherDiscard, err := other.StageDir("txn-other")
	if err != nil {
		t.Fatal(err)
	}
	defer otherDiscard()

	if err := svc.DismissResumable("txn-dismiss"); err != nil {
		t.Fatalf("DismissResumable: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("被忽略事务的 staging 应已按背书清理")
	}
	if _, err := os.Stat(otherStaging); err != nil {
		t.Error("其他事务现场绝不允许被误删")
	}
	j, err := store.Get("txn-dismiss")
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed || j.Error == nil || j.Error.Code != "resumable-dismissed" {
		t.Errorf("忽略应以 failed + resumable-dismissed 收口: %+v", j)
	}
	for _, op := range svc.ListOperations() {
		if strings.HasPrefix(op.ID, "resumed-txn-dismiss") {
			t.Fatalf("忽略后 resumable 回灌记录应被摘除: %+v", op)
		}
	}
	// 幂等重放：已 failed 的账再次忽略不报错（现场清理天然 no-op）
	if err := svc.DismissResumable("txn-dismiss"); err != nil {
		t.Errorf("重复忽略应幂等, got %v", err)
	}
}

func TestDismissResumableRejectsClosedAndUnknown(t *testing.T) {
	svc, store, _ := newOpService(t)
	// 已成功收口的历史账：拒绝翻案
	doneID := "txn-succeeded"
	if err := store.Begin(operation.Journal{
		TransactionID: doneID, Operation: operation.TxnOpUpdate,
		DeliveryKind: extapi.DeliveryManagedDeclarative, ModuleID: "markeron", Phase: "resolve",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(doneID, string(operation.TxnSucceeded), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DismissResumable(doneID); err == nil ||
		!strings.Contains(err.Error(), "翻案") {
		t.Errorf("对已成功账应拒绝翻案, got %v", err)
	}
	if err := svc.DismissResumable("txn-ghost-not-exist"); err == nil {
		t.Error("未知事务应报错")
	}
	if err := svc.DismissResumable("  "); err == nil {
		t.Error("空 ID 应报错")
	}
}

func findOp(list []extapi.Operation, id string) (extapi.Operation, bool) {
	for _, op := range list {
		if op.ID == id {
			return op, true
		}
	}
	return extapi.Operation{}, false
}

func TestListOperationsNilHubSafe(t *testing.T) {
	svc, _, _ := newTestAppService(t, "alpha")
	if got := svc.ListOperations(); got != nil {
		t.Errorf("未注入观察面应回 nil, got %+v", got)
	}
	if err := svc.DismissResumable("txn-x"); err == nil {
		t.Error("未注入忽略通道应如实报错（不静默成功）")
	}
}
