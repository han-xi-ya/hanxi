// 安装事务 journal 链路测试（与 markeron 同构，资产形态为便携 zip、下载走
// 无摘要降级链）：假源驱动 Begin→Advance→Complete 全步、中断失败收口、
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

// windtermTestSteps 与 WindTermService.windtermInstallSteps 同一词表（服务层
// 编排在本包不可达，此处复刻同款阶段映射以验证交接面）。
var windtermTestSteps = []string{"download", "unpack", "place"}

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
	txn, err := ops.BeginTxn("windterm", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, windtermTestSteps)
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

func TestJournalChainSuccess(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)

	txnID := newTxnID()
	zipBytes := buildPortableZip(t, "0.10.0", "journal-exe")
	seedRemote(t, "v0.10.0", "WindTerm_0.10.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false, false)

	if err := driveInstall(t, m, txnID, "v0.10.0"); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.Operation != operation.TxnOpInstall ||
		j.DeliveryKind != extapi.DeliveryManagedDeclarative || j.ModuleID != "windterm" {
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
	assertNoLeftovers(t, m.versionsDir)

	_, hub := ops.Kernel()
	rec := hub.Recent("windterm", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
}

func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)
	// 先正例装一版（旧版完好基线）
	oldZip := buildPortableZip(t, "0.9.2", "old-exe")
	seedRemote(t, "v0.9.2", "WindTerm_0.9.2_Windows_Portable_x86_64.zip", int64(len(oldZip)), "")
	src.set(oldZip, false, false)
	if err := driveInstall(t, m, newTxnID(), "v0.9.2"); err != nil {
		t.Fatal(err)
	}

	txnID := newTxnID()
	newZip := buildPortableZip(t, "0.10.0", "interrupted")
	seedRemote(t, "v0.10.0", "WindTerm_0.10.0_Windows_Portable_x86_64.zip", int64(len(newZip)), "")
	src.set(newZip, true, false) // 半路掐线

	if err := driveInstall(t, m, txnID, "v0.10.0"); err == nil {
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
	assertNoLeftovers(t, m.versionsDir)
	if list, _ := m.ListInstalled(); len(list) != 1 || list[0].Version != "v0.9.2" {
		t.Fatalf("旧版本必须完好: %+v", list)
	}
}

func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	if _, err := ops.BeginTxn("windterm", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "v0.9.2", newTxnID(), windtermTestSteps); err != nil {
		t.Fatal(err)
	}
	_, err := ops.BeginTxn("windterm", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "v0.10.0", newTxnID(), windtermTestSteps)
	if err == nil || !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}

func TestJournalCrashRecoveryEndorsement(t *testing.T) {
	store := withTestKernel(t)
	m := NewManager(t.TempDir())

	txnID := newTxnID()
	txn, err := ops.BeginTxn("windterm", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"v0.10.0", txnID, windtermTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0)
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟强杀：不收口、不丢弃
	if err := os.MkdirAll(filepath.Join(staging, "WindTerm_0.10.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "WindTerm_0.10.0", exeName), []byte("half"), 0644); err != nil {
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

	trees := map[string]*artifact.Tree{"windterm": OpenTree(m.versionsDir)}
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
	rec := hub2.Recent("windterm", 0)
	if len(rec) != 1 || rec[0].Error == nil || rec[0].Error.Code != "resumable" {
		t.Fatalf("恢复后应回灌 resumable 记录: %+v", rec)
	}
}
