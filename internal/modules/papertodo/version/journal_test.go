// Wave 4-B 安装事务 journal 链路测试（ccswitch/rufus 同构）：以假下载源驱动
// Begin→Advance→Complete 全步、失败收口、强杀模拟（重启 Store 后 Pending 可见）、
// 启动恢复"背书"清理（只删本事务 staging，无背书孤儿只 Report 不动盘）与
// 单写事务拒并发。全部落真 tmp 目录 + 回环假源，无网络依赖。
//
// papertodo 差异登记：落位是"固定托管目录覆盖换入"（非 Tree 版本目录），
// 事务现场 = 版本树根下的 .tmp-<txnID> staging 目录，背书清理语义不变。
package version

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// papertodoTestSteps 与 PaperTodoService.papertodoInstallSteps 保持同一词表
// （服务层编排在本包不可达，此处复刻同款阶段映射以验证交接面）。
var papertodoTestSteps = []string{"download", "verify", "place"}

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

// driveInstall 复刻 service.DownloadVersion 后台协程的事务编排：
// BeginTxn → 阶段迁移 Step/Progress → Done/Fail。
func driveInstall(t *testing.T, m *Manager, txnID, version, variant string) error {
	t.Helper()
	txn, err := ops.BeginTxn("papertodo", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		version, txnID, papertodoTestSteps)
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
		}
	}
	if err := m.Download(txnID, version, variant, emit); err != nil {
		txn.Fail("asset-install-failed", err.Error())
		return err
	}
	txn.Done()
	return nil
}

// TestJournalChainSuccess 正例全步链：journal 从 resolve 起步、download/verify
// 两步经事件推进、Done 收口全部 succeeded（place 步无独立事件阶段，由收口
// 记成——与 ccswitch 的 place 同构）；观察面记录同步落终态。
func TestJournalChainSuccess(t *testing.T) {
	store := withTestKernel(t)
	src := newExeSource(t)
	m := newChainManager(t, src)

	txnID := newTxnID()
	exeBytes := fakeExe("journal-exe")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", exeBytes, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(exeBytes, false)

	if err := driveInstall(t, m, txnID, "v3.31", VariantSelfContained); err != nil {
		t.Fatalf("driveInstall: %v", err)
	}

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.Operation != operation.TxnOpInstall ||
		j.DeliveryKind != extapi.DeliveryManagedDeclarative || j.ModuleID != "papertodo" {
		t.Errorf("账本终态异常: %+v", j)
	}
	if j.ToVersion == nil || *j.ToVersion != "v3.31" {
		t.Errorf("ToVersion 入账缺失: %+v", j)
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
	rec := hub.Recent("papertodo", 10)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded || rec[0].Kind != extapi.OpInstall {
		t.Fatalf("观察面记录异常: %+v", rec)
	}
	if rec[0].Progress == nil || *rec[0].Progress != 100 {
		t.Errorf("Done 应补满进度: %+v", rec[0].Progress)
	}
}

// TestJournalChainFailureInterrupt 中断失败链：journal failed + 错误入账、
// download 步记 failed；盘上无半件、托管目录不被触碰。
func TestJournalChainFailureInterrupt(t *testing.T) {
	store := withTestKernel(t)
	src := newExeSource(t)
	m := newChainManager(t, src)

	// 先装一个旧版：失败链必须分毫不动它
	installViaChain(t, m, src, "v3.3", VariantSelfContained, "old-before-crash")
	oldPath := filepath.Join(m.InstallDir(), exeName)
	oldBefore, _ := os.ReadFile(oldPath)

	txnID := newTxnID()
	exeBytes := fakeExe("interrupted-payload")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", exeBytes, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(exeBytes, true) // 半路掐线

	if err := driveInstall(t, m, txnID, "v3.31", VariantSelfContained); err == nil {
		t.Fatal("应失败")
	}
	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnFailed {
		t.Fatalf("state = %s, want failed", j.State)
	}
	if j.Error == nil || j.Error.Code != "asset-install-failed" || j.Error.Message == "" {
		t.Errorf("失败错误未入账: %+v", j.Error)
	}
	if len(j.Steps) == 0 || j.Steps[0].State != operation.TxnFailed {
		t.Errorf("download 步应记 failed: %+v", j.Steps)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldAfter, _ := os.ReadFile(oldPath)
	if string(oldAfter) != string(oldBefore) || len(oldBefore) == 0 {
		t.Error("失败链不得触碰托管目录现有 exe")
	}
}

// TestJournalSingleWriteRejectsConcurrent 同一 moduleId 未收口事务占位时，
// 第二笔 BeginTxn 必须被单写纪律拒绝（ErrTxnActive 透传）。
func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	_, err := ops.BeginTxn("papertodo", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "v3.31", newTxnID(), papertodoTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	// 故意不收口：模拟在途/崩溃现场，再开第二笔
	_, err = ops.BeginTxn("papertodo", extapi.OpUpdate, extapi.DeliveryManagedDeclarative, "v3.32", newTxnID(), papertodoTestSteps)
	if err == nil || !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}

// TestJournalCrashRecoveryEndorsement 强杀模拟：事务 Begin+Step 后进程"死亡"
// （不收口、staging 半件留在盘上）→ 重启后新 Store 的 Pending 可见该账；
// Recover 按背书只清理该事务的 .tmp-<txnID> 目录并 Complete(compensated)；
// 托管目录不被误伤；重建观察面后以 resumable 呈现待用户处置。
func TestJournalCrashRecoveryEndorsement(t *testing.T) {
	store := withTestKernel(t)
	src := newExeSource(t)
	m := newChainManager(t, src)

	// 已有托管安装：恢复清理绝不允许波及固定目录内容（含便签数据）
	installViaChain(t, m, src, "v3.3", VariantSelfContained, "installed-before-crash")
	if err := os.WriteFile(filepath.Join(m.InstallDir(), dataFileName), []byte(`{"papers":["endure"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	txnID := newTxnID()
	txn, err := ops.BeginTxn("papertodo", extapi.OpInstall, extapi.DeliveryManagedDeclarative,
		"v3.31", txnID, papertodoTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	txn.Step(0)
	// 半件 staging：真实事务目录名 + 半个 exe，然后模拟强杀（不收口、不 discard）
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		t.Fatal(err)
	}
	_ = discard
	if err := os.WriteFile(filepath.Join(staging, exeName), fakeExe("half-killed"), 0o644); err != nil {
		t.Fatal(err)
	}

	// —— "重启"：同目录新开 Store，Pending 应见未收口账 ——
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

	report, err := operation.Recover(reopened, crashCompensate(reopened, m.versionsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RolledBack) != 1 || report.RolledBack[0].State != operation.TxnCompensated {
		t.Fatalf("背书清理应落 compensated: %+v", report)
	}
	if fileExists(filepath.Join(m.versionsDir, ".tmp-"+txnID)) {
		t.Fatal("该事务的 staging 半件应被背书清理")
	}
	// 托管安装与便签数据原样完好（背书清理只认事务现场）
	if got, rerr := os.ReadFile(filepath.Join(m.InstallDir(), exeName)); rerr != nil || !strings.Contains(string(got), "installed-before-crash") {
		t.Fatalf("背书清理不得触碰托管目录: %v %q", rerr, got)
	}
	if got, rerr := os.ReadFile(filepath.Join(m.InstallDir(), dataFileName)); rerr != nil || string(got) != `{"papers":["endure"]}` {
		t.Fatalf("便签数据必须熬过崩溃恢复: %v %q", rerr, got)
	}
	if list, lerr := m.ListInstalled(); lerr != nil || len(list) != 1 || list[0].Version != "v3.3" {
		t.Fatalf("ListInstalled 不得见半件、不得丢安装: %+v %v", list, lerr)
	}

	// compensated 仍留在 Pending：重建观察面后以 resumable 回灌呈现（等待忽略收口）
	hub2 := operation.NewHub(reopened)
	rec := hub2.Recent("papertodo", 0)
	if len(rec) != 1 || rec[0].Error == nil || rec[0].Error.Code != "resumable" || !rec[0].Error.Recoverable {
		t.Fatalf("恢复后应回灌 resumable 记录: %+v", rec)
	}
}

// TestJournalUnbackedResidueOnlyReported 无背书残骸：.tmp-* 目录没有对应未收口
// 账本时，Recover 不接管（无 journal 可派）、恢复流程绝不代删——只经
// ListUnbackedTxnDirs 如实上报。
func TestJournalUnbackedResidueOnlyReported(t *testing.T) {
	store := withTestKernel(t)
	m := newChainManager(t, newExeSource(t))

	ghost, _, err := m.tree.StageDir("ghost-txn")
	if err != nil {
		t.Fatal(err)
	}

	report, err := operation.Recover(store, crashCompensate(store, m.versionsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Resumed)+len(report.RolledBack)+len(report.Orphaned) != 0 {
		t.Fatalf("无账可恢复时报告应为空: %+v", report)
	}
	if !fileExists(ghost) {
		t.Fatal("无背书残骸绝不允许被自动删除")
	}
	unbacked := ops.ListUnbackedTxnDirs(m.tree, map[string]bool{})
	if len(unbacked) != 1 || !strings.HasPrefix(unbacked[0], ".tmp-ghost-txn") {
		t.Fatalf("无背书残骸应被盘点上报: %v", unbacked)
	}
}

// crashCompensate 复刻 app.go 装配根的补偿器形态（背书清理 + compensated 落账）。
func crashCompensate(store *operation.Store, versionsDir string) operation.Compensation {
	trees := map[string]*artifact.Tree{"papertodo": OpenTree(versionsDir)}
	return func(j operation.Journal) (bool, error) {
		tree, ok := trees[j.ModuleID]
		if !ok {
			return false, nil
		}
		if err := ops.CleanTxnResidue(tree, j.TransactionID); err != nil {
			return true, err
		}
		return true, store.Complete(j.TransactionID, string(operation.TxnCompensated), nil)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
