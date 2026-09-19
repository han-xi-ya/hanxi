// Wave 4-B 安装事务 journal 链路测试（与 markeron/ccswitch/translucenttb 同构、
// 资产形态为 NSIS 静默安装特例）：假下载源 + 假 nsisInstall 驱动
// Begin→Advance→Complete 全步、中断失败收口、并发单写拒绝与崩溃残留如实上报。
// 本模块刻意无 artifact.Tree（覆盖式单目录，版本树模型表达不了），恢复背书
// 面不登记 recordly——崩溃未收口事务按"未认领、只上报"口径处理，用例锁定
// 该形态与 operation.Recover 的交接行为。全部真 tmp 目录 + 回环假源，无网络依赖。
package version

import (
	"errors"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/operation"
)

// recordlyTestSteps 与 RecordlyService.recordlyInstallSteps 同一词表（服务层
// 编排在本包不可达，此处复刻同款阶段映射以验证交接面：download（内核 Fetch
// 传输+摘要双核）/verify（SHA256SUMS 第二只眼）/install（NSIS 静默+布局自检））。
var recordlyTestSteps = []string{"download", "verify", "install"}

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
	txn, err := ops.BeginTxn("recordly", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, recordlyTestSteps)
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
			if stepIdx < 1 {
				stepIdx = 1
				txn.Step(stepIdx)
			}
		case "install":
			if stepIdx < 2 {
				stepIdx = 2
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
	src := newInstallerSource(t)
	m := newChainManager(t, src)

	txnID := newTxnID()
	instBytes := []byte("nsis-installer:journal")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)

	if err := driveInstall(t, m, txnID, "v1.3.5"); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.Operation != operation.TxnOpInstall ||
		j.DeliveryKind != extapi.DeliveryManagedDeclarative || j.ModuleID != "recordly" {
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
	rec := hub.Recent("recordly", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
}

// TestJournalChainFailureInterrupt 中断失败链：journal failed + 错误入账，
// download 步记 failed；旧安装完好（NSIS 根本未被执行）。
func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.3")

	txnID := newTxnID()
	newBytes := []byte("nsis-installer:interrupted")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(newBytes)), shaHex(newBytes))
	src.set(newBytes, true) // 半路掐线

	if err := driveInstall(t, m, txnID, "v1.3.5"); err == nil {
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
	if _, err := ops.BeginTxn("recordly", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "v1.3.3", newTxnID(), recordlyTestSteps); err != nil {
		t.Fatal(err)
	}
	_, err := ops.BeginTxn("recordly", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "v1.3.5", newTxnID(), recordlyTestSteps)
	if err == nil || !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}

// TestJournalCrashPendingUnbackedReported 强杀模拟：Begin+Step 后不收口 →
// 重启 Store 的 Pending 可见；recordly 无 artifact.Tree、装配根背书清理面
// 不登记（覆盖式单目录无 .tmp staging 可定位），Recover 按"不猜测、不自动
// 执行"把该事务如实列入 Orphaned，等待用户经操作观察面处置。
func TestJournalCrashPendingUnbackedReported(t *testing.T) {
	store := withTestKernel(t)

	txnID := newTxnID()
	txn, err := ops.BeginTxn("recordly", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"v1.3.5", txnID, recordlyTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0) // 不收口：模拟安装协程中途强杀

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

	report, err := operation.Recover(reopened, func(operation.Journal) (bool, error) {
		return false, nil // 装配根对未登记模块的默认口径：不接管
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orphaned) != 1 || report.Orphaned[0].TransactionID != txnID {
		t.Fatalf("无背书模块的未收口事务应如实列入 Orphaned: %+v", report)
	}
}
