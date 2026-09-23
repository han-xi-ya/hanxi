// 安装事务 journal 链路测试（与 markeron/windterm 同构，官方摘要主链）：
// fetch 接缝驱动 Begin→Advance→Complete 全步、中断失败收口、强杀模拟
// （重启 Store 后 Pending 可见）与启动恢复"背书"清理。全部真 tmp 目录。
package version

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// termoraTestSteps 与 TermoraService.termoraInstallSteps 同一词表。
var termoraTestSteps = []string{"download", "unpack", "place"}

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
	txn, err := ops.BeginTxn("termora", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, termoraTestSteps)
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
	zipBytes := buildPortableZip(t, "journal-exe")
	digest := strings.Repeat("a", 64)
	seedRemote(t, "v0.10.0", "termora-0.10.0-windows-x86-64.zip", int64(len(zipBytes)), digest)
	m := NewManager(t.TempDir())
	fakeFetch(m, zipBytes, digest, nil)

	txnID := newTxnID()
	if err := driveInstall(t, m, txnID, "v0.10.0"); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.ModuleID != "termora" || j.DeliveryKind != extapi.DeliveryManagedDeclarative {
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
	rec := hub.Recent("termora", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
}

func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	m := NewManager(t.TempDir())
	// 正例基线
	oldZip := buildPortableZip(t, "old")
	seedRemote(t, "v0.9.2", "termora-0.9.2-windows-x86-64.zip", int64(len(oldZip)), strings.Repeat("b", 64))
	fakeFetch(m, oldZip, strings.Repeat("b", 64), nil)
	if err := driveInstall(t, m, newTxnID(), "v0.9.2"); err != nil {
		t.Fatal(err)
	}

	txnID := newTxnID()
	seedRemote(t, "v0.10.0", "termora-0.10.0-windows-x86-64.zip", 1234, strings.Repeat("c", 64))
	m.fetch = func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
		return errors.New("下载中断（模拟传输半路掐线）")
	}
	if err := driveInstall(t, m, txnID, "v0.10.0"); err == nil {
		t.Fatal("应失败")
	}
	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed || j.Error == nil || j.Error.Code != "asset-install-failed" {
		t.Fatalf("失败账目异常: %+v", j)
	}
	assertNoLeftovers(t, m.versionsDir)
	if list, _ := m.ListInstalled(); len(list) != 1 || list[0].Version != "v0.9.2" {
		t.Fatalf("旧版本必须完好: %+v", list)
	}
}

func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	if _, err := ops.BeginTxn("termora", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "v0.9.2", newTxnID(), termoraTestSteps); err != nil {
		t.Fatal(err)
	}
	_, err := ops.BeginTxn("termora", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "v0.10.0", newTxnID(), termoraTestSteps)
	if err == nil || !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}

func TestJournalCrashRecoveryEndorsement(t *testing.T) {
	store := withTestKernel(t)
	m := NewManager(t.TempDir())

	txnID := newTxnID()
	txn, err := ops.BeginTxn("termora", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"v0.10.0", txnID, termoraTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0)
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟强杀：不收口、不丢弃
	if err := os.MkdirAll(filepath.Join(staging, payloadDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, payloadDir, exeName), []byte("half"), 0644); err != nil {
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

	trees := map[string]*artifact.Tree{"termora": OpenTree(m.versionsDir)}
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
}
