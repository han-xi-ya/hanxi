// Wave 4-B 安装事务 journal 链路测试（与 ccswitch/markeron 同构、资产形态为
// GitHub 官方摘要便携 zip）：假下载源驱动 Begin→Advance→Complete 全步、
// 中断失败收口、落位防漂移失败归因、强杀模拟（重启 Store 后 Pending 可见）
// 与启动恢复"背书"清理。官方摘要双核由内核 Fetch 原子折进 download 步，
// journal 不造幻影 verify 步（进度事件的 verify 阶段仍照常发出，前端词表）。
// 全部真 tmp 目录 + 回环假源，无网络依赖。
package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// bili23TestSteps 与 Service.bili23InstallSteps 同一词表（服务层编排在本包
// 不可达，此处复刻同款阶段映射以验证交接面）。
var bili23TestSteps = []string{"download", "unpack", "place"}

// withTestKernel 注入真 tmp 账本 + 观察面，测试结束复位全局内核（本包测试串行）。
func withTestKernel(t *testing.T) *operation.Store {
	t.Helper()
	store, err := operation.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ops.SetKernel(store, operation.NewHub(store))
	t.Cleanup(func() { ops.SetKernel(nil, nil) })
	return store
}

// driveInstall 复刻 service.DownloadVersion 后台协程的事务编排。
func driveInstall(t *testing.T, m *Manager, txnID, version string) error {
	t.Helper()
	txn, err := ops.BeginTxn("bili23", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, bili23TestSteps)
	if err != nil {
		return err
	}
	stepIdx := -1
	emit := func(p DownloadProgress) {
		switch p.Stage {
		case "downloading":
			if stepIdx < 0 {
				stepIdx = 0
				txn.Step(stepIdx)
			}
			txn.Progress(p.Done, p.Total)
		case "extract":
			if stepIdx < 1 {
				stepIdx = 1
				txn.Step(stepIdx)
			}
		}
	}
	if err := m.Download(txnID, version, emit); err != nil {
		txn.Fail("asset-install-failed", err.Error())
		return err
	}
	txn.Done()
	return nil
}

// TestJournalChainSuccess 正例全步链：步进推进、succeeded 收口；观察面记录同步。
func TestJournalChainSuccess(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)

	txnID := newTxnID()
	zipBytes := buildPortableZip(t, "fake-exe")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	if err := driveInstall(t, m, txnID, "v2.15.0"); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.Operation != operation.TxnOpInstall ||
		j.DeliveryKind != extapi.DeliveryManagedDeclarative || j.ModuleID != "bili23" {
		t.Errorf("账本终态异常: %+v", j)
	}
	if j.Phase != "done" || len(j.Steps) != 3 {
		t.Fatalf("步骤账异常: %+v", j.Steps)
	}
	for _, s := range j.Steps {
		if s.State != operation.TxnSucceeded {
			t.Errorf("步骤 %s 未收口: %+v", s.Name, s)
		}
	}
	assertNoTransactionLeftovers(t, m.versionsDir)

	_, hub := ops.Kernel()
	rec := hub.Recent("bili23", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
}

// TestJournalChainFailureInterrupt 中断失败链：journal failed + 错误入账，
// download 步记 failed；盘上无半件、旧版完好。
func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.14.0", "old-exe-bytes")

	txnID := newTxnID()
	zipBytes := buildPortableZip(t, "brand-new-exe")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, true) // 半路掐线

	if err := driveInstall(t, m, txnID, "v2.15.0"); err == nil {
		t.Fatal("应失败")
	}
	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed {
		t.Fatalf("state = %s, want failed", j.State)
	}
	if j.Error == nil || j.Error.Code != "asset-install-failed" {
		t.Errorf("失败错误未入账: %+v", j.Error)
	}
	if len(j.Steps) == 0 || j.Steps[0].State != operation.TxnFailed {
		t.Errorf("download 步应记 failed: %+v", j.Steps)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestJournalChainFailurePlaceAttribution 失败发生在落位段（同版本重传异摘要，
// Tree.Commit 防漂移拒装）：download 步已 succeeded、unpack 步记 failed——
// 摘要双核折进 download 步、Commit 无独立进度事件，归因如实反映阶段映射。
func TestJournalChainFailurePlaceAttribution(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.15.0", "same-exe")

	txnID := newTxnID()
	tampered := buildPortableZip(t, "same-exe-tampered")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)

	if err := driveInstall(t, m, txnID, "v2.15.0"); err == nil {
		t.Fatal("应失败")
	}
	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed || len(j.Steps) < 2 {
		t.Fatalf("账本终态异常: %+v", j)
	}
	if j.Steps[0].State != operation.TxnSucceeded {
		t.Errorf("download 步应已收口: %+v", j.Steps[0])
	}
	if j.Steps[1].State != operation.TxnFailed {
		t.Errorf("unpack 步应记 failed: %+v", j.Steps[1])
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestJournalSingleWriteRejectsConcurrent 未收口事务占位时第二笔 BeginTxn
// 必须被单写纪律拒绝（ErrTxnActive 透传给调用方呈现）。
func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	if _, err := ops.BeginTxn("bili23", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "v2.14.0", newTxnID(), bili23TestSteps); err != nil {
		t.Fatal(err)
	}
	_, err := ops.BeginTxn("bili23", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "v2.15.0", newTxnID(), bili23TestSteps)
	if err == nil || !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}

// TestJournalCrashRecoveryEndorsement 强杀模拟：Begin+Step 后不收口、staging
// 半件留盘 → 重启 Store 的 Pending 可见；Recover 背书只删本事务 .tmp-<txnID>、
// 落 compensated；版本树无半件；重建观察面后以 resumable 回灌。
func TestJournalCrashRecoveryEndorsement(t *testing.T) {
	store := withTestKernel(t)
	m := NewManager(t.TempDir())

	txnID := newTxnID()
	txn, err := ops.BeginTxn("bili23", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"v2.15.0", txnID, bili23TestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0)
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟强杀：不收口、不丢弃
	if err := os.WriteFile(filepath.Join(staging, exeName), []byte("half"), 0644); err != nil {
		t.Fatal(err)
	}

	reopened, err := operation.OpenStore(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TransactionID != txnID || pending[0].Phase != "download" {
		t.Fatalf("重启后 Pending 应可见崩溃事务: %+v", pending)
	}

	trees := map[string]*artifact.Tree{"bili23": OpenTree(m.versionsDir)}
	report, err := operation.Recover(reopened, func(j operation.Journal) (bool, error) {
		tree, ok := trees[j.ModuleID]
		if !ok {
			return false, nil
		}
		if err := ops.CleanTxnResidue(tree, j.TransactionID); err != nil {
			return true, err
		}
		return true, reopened.Complete(j.TransactionID, string(operation.TxnCompensated), nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RolledBack) != 1 || report.RolledBack[0].State != operation.TxnCompensated {
		t.Fatalf("背书清理应落 compensated: %+v", report)
	}
	if _, serr := os.Stat(filepath.Join(m.versionsDir, ".tmp-"+txnID)); serr == nil {
		t.Fatal("该事务的 staging 半件应被背书清理")
	}
	if list, lerr := m.ListInstalled(); lerr != nil || len(list) != 0 {
		t.Fatalf("ListInstalled 不得见半件: %+v %v", list, lerr)
	}

	hub2 := operation.NewHub(reopened)
	rec := hub2.Recent("bili23", 0)
	if len(rec) != 1 || rec[0].Error == nil || rec[0].Error.Code != "resumable" {
		t.Fatalf("恢复后应回灌 resumable 记录: %+v", rec)
	}
}
