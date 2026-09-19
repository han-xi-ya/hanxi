// Wave 4-B 安装事务 journal 链路测试（与 markeron/ccswitch 同构、资产形态为
// 官网便携 zip）：假下载源驱动 Begin→Advance→Complete 全步、中断失败收口、
// 强杀模拟（重启 Store 后 Pending 可见）与启动恢复"背书"清理。
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

// everythingTestSteps 与 EverythingService.everythingInstallSteps 同一词表（服务层
// 编排在本包不可达，此处复刻同款阶段映射以验证交接面；官方摘要双核（verify）
// 由内核 Fetch 折进 download 步内完成——journal 步骤词表不为其单列，与模块
// 进度事件词表（含 verify）各司其职）。
var everythingTestSteps = []string{"download", "unpack", "place"}

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
	txn, err := ops.BeginTxn("everything", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, everythingTestSteps)
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
		case "verify":
			// 摘要双核仍属 download 步内事件：不回退步进、不造幻影步骤
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

// TestJournalChainSuccess 正例全步链：三步推进、succeeded 收口；观察面记录同步。
func TestJournalChainSuccess(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t)

	txnID := newTxnID()
	zipBytes := buildPortableZip(t, "Everything.exe", "journal-exe")
	seedRemote(t, "1.5.0.1422b", int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName("1.5.0.1422b")))
	src.set(zipBytes, false)

	if err := driveInstall(t, m, txnID, "1.5.0.1422b"); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.Operation != operation.TxnOpInstall ||
		j.DeliveryKind != extapi.DeliveryManagedDeclarative || j.ModuleID != "everything" {
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
	rec := hub.Recent("everything", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
}

// TestJournalChainFailureInterrupt 中断失败链：journal failed + 错误入账，
// download 步记 failed；盘上无半件、旧版完好。
func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t)
	installViaChain(t, m, src, "1.4.1.1032", "everything.exe", "old-exe")

	txnID := newTxnID()
	newZip := buildPortableZip(t, "Everything.exe", "interrupted")
	seedRemote(t, "1.5.0.1422b", int64(len(newZip)), shaHex(newZip), src.url(assetName("1.5.0.1422b")))
	src.set(newZip, true) // 半路掐线

	if err := driveInstall(t, m, txnID, "1.5.0.1422b"); err == nil {
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

// TestJournalSingleWriteRejectsConcurrent 未收口事务占位时第二笔 BeginTxn
// 必须被单写纪律拒绝（ErrTxnActive 透传给调用方呈现）。
func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	if _, err := ops.BeginTxn("everything", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "1.4.1.1032", newTxnID(), everythingTestSteps); err != nil {
		t.Fatal(err)
	}
	_, err := ops.BeginTxn("everything", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "1.5.0.1422b", newTxnID(), everythingTestSteps)
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
	txn, err := ops.BeginTxn("everything", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"1.5.0.1422b", txnID, everythingTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0)
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟强杀：不收口、不丢弃
	if err := os.WriteFile(filepath.Join(staging, "Everything.exe"), []byte("half"), 0644); err != nil {
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

	trees := map[string]*artifact.Tree{"everything": OpenTree(m.versionsDir)}
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
	rec := hub2.Recent("everything", 0)
	if len(rec) != 1 || rec[0].Error == nil || rec[0].Error.Code != "resumable" {
		t.Fatalf("恢复后应回灌 resumable 记录: %+v", rec)
	}
}
