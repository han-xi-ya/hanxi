package operation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hanxi/internal/extapi"
)

// seedTxn 以托管事务的标准姿势落一笔 journal 底账。
func seedTxn(t *testing.T, s *Store, txn, module string, op TxnOperation) {
	t.Helper()
	j := testJournal(txn, module)
	j.Operation = op
	if err := s.Begin(j); err != nil {
		t.Fatalf("seed Begin %s: %v", txn, err)
	}
}

// TestHubReflowPendingAsResumable §2.3 刷新恢复：NewHub 把未收口事务回灌为
// failed + error.code=resumable + recoverable=true；kind 由 journal operation
// 派生（uninstall→remove）；不进 Active。
func TestHubReflowPendingAsResumable(t *testing.T) {
	s, _ := newTestStore(t)
	seedTxn(t, s, "tx-inst", "markeron", TxnOpInstall)
	txUn := testJournal("tx-unin", "ccswitch") // 同模块单写事务：不同模块才能并存
	txUn.Operation, txUn.Phase = TxnOpUninstall, "remove"
	if err := s.Begin(txUn); err != nil {
		t.Fatal(err)
	}
	seedTxn(t, s, "tx-else", "rufus", TxnOpUpdate)

	h := NewHub(s)
	if act := h.Active(); len(act) != 0 {
		t.Fatalf("回灌合成记录不得进 Active: %+v", act)
	}
	got := h.Recent("", 10) // 全模块
	if len(got) != 3 {
		t.Fatalf("回灌数 %d != 3: %+v", len(got), got)
	}
	byID := map[string]extapi.Operation{}
	for _, op := range got {
		byID[op.ID] = op
	}
	for id, want := range map[string]struct {
		kind   extapi.OperationKind
		module string
	}{
		"resumed-tx-inst": {extapi.OpInstall, "markeron"},
		"resumed-tx-unin": {extapi.OpRemove, "ccswitch"},
	} {
		op, ok := byID[id]
		if !ok {
			t.Fatalf("缺回灌记录 %s: %+v", id, got)
		}
		if op.Status != extapi.OpFailed || op.Kind != want.kind {
			t.Errorf("%s: status=%s kind=%s", id, op.Status, want.kind)
		}
		if op.Error == nil || op.Error.Code != "resumable" || !op.Error.Recoverable {
			t.Errorf("%s: 非 resumable 呈现: %+v", id, op.Error)
		}
		if op.ModuleID != want.module || op.Schema != extapi.ModuleContractSchema {
			t.Errorf("%s: 归属/schema 不对: %+v", id, op)
		}
	}
	if len(h.Recent("nosuch", 5)) != 0 {
		t.Error("Recent 模块过滤不对")
	}
}

// TestKindMapping kind↔journal 枚举双向映射逐值冻结；activate/stop/invoke 无
// journal 词汇（Wave 0 备忘 2：持久化排除面由类型映射强制）。
func TestKindMapping(t *testing.T) {
	cases := []struct {
		kind extapi.OperationKind
		op   TxnOperation
		ok   bool
	}{
		{extapi.OpInstall, TxnOpInstall, true},
		{extapi.OpUpdate, TxnOpUpdate, true},
		{extapi.OpRollback, TxnOpRollback, true},
		{extapi.OpRepair, TxnOpRepair, true},
		{extapi.OpRemove, TxnOpUninstall, true},
		{extapi.OpActivate, "", false},
		{extapi.OpStop, "", false},
		{extapi.OpInvoke, "", false},
	}
	for _, tc := range cases {
		got, ok := journalOperation(tc.kind)
		if ok != tc.ok || got != tc.op {
			t.Errorf("journalOperation(%s) = (%q,%v), 期望 (%q,%v)", tc.kind, got, ok, tc.op, tc.ok)
			continue
		}
		if !tc.ok {
			continue // 无 journal 词汇者不参与逆映射
		}
		back, bok := operationKindOf(tc.op)
		if !bok || back != tc.kind {
			t.Errorf("operationKindOf(%s) = (%q,%v), 期望 (%q,true)", tc.op, back, bok, tc.kind)
		}
	}
}

// TestHubReflowSameModuleTwoPending 账本里真出现同 moduleId 的两笔未收口事务
// （手工旁路写入构造——正是单写纪律被崩溃/旁路破坏后的现场形态）时，NewHub
// 仍逐笔生成 resumable 记录：ID 由 "resumed-"+transactionId 保证互不相同。
func TestHubReflowSameModuleTwoPending(t *testing.T) {
	s, dir := newTestStore(t)
	j1 := testJournal("txn-pa", "markeron")
	j2 := testJournal("txn-pb", "markeron")
	for _, j := range []Journal{j1, j2} {
		data, err := marshalJournal(&j)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, j.TransactionID+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHub(s)
	seen := map[string]bool{}
	for _, op := range h.Recent("markeron", 0) {
		if seen[op.ID] {
			t.Fatalf("回灌 ID 撞车: %s", op.ID)
		}
		seen[op.ID] = true
		if op.Status != extapi.OpFailed || op.Error == nil || op.Error.Code != "resumable" {
			t.Errorf("非 resumable 呈现: %+v", op)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("期望两条不同 ID 的 resumable 记录，实得 %v", seen)
	}
}

// TestHubForgetResumable 宿主代收口一笔回灌事务后，ForgetResumable 摘除其
// resumable 合成记录：Recent 不再呈现该记录、返回 true；未知/已摘除 ID 返回
// false 且不误伤其他回灌记录；真实在途记录（Handle）不受影响。
func TestHubForgetResumable(t *testing.T) {
	s, _ := newTestStore(t)
	seedTxn(t, s, "tx-a", "markeron", TxnOpInstall)
	seedTxn(t, s, "tx-b", "rufus", TxnOpUpdate)
	if err := s.Complete("tx-a", string(TxnFailed), &extapi.OperationError{Code: "resumable-dismissed", Message: "用户忽略"}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(s) // tx-a 已 failed 收口 → 只有 tx-b 进回灌
	if len(h.Recent("", 0)) != 1 {
		t.Fatalf("回灌基线异常: %+v", h.Recent("", 0))
	}
	if !h.ForgetResumable("tx-b") {
		t.Fatal("应摘除 tx-b 的回灌记录")
	}
	if got := len(h.Recent("", 0)); got != 0 {
		t.Fatalf("摘除后回灌应为空, got %d", got)
	}
	if h.ForgetResumable("tx-b") || h.ForgetResumable("no-such-txn") {
		t.Error("未知/重复摘除应返回 false")
	}
	// 真实在途记录不受影响
	hd := h.Begin("markeron", extapi.OpInstall, "")
	hd.Phase("download")
	if len(h.Recent("", 0)) != 1 || h.Active()[0].ID != hd.op.ID {
		t.Error("ForgetResumable 不得动真实记录")
	}
}

// TestHubNilStore NewHub(nil) 纯内存可用，终态不触任何磁盘。
func TestHubNilStore(t *testing.T) {
	h := NewHub(nil)
	hd := h.Begin("markeron", extapi.OpActivate, "ignored-txn")
	hd.Done()
	if len(h.Active()) != 0 || len(h.Recent("markeron", 5)) != 1 {
		t.Fatal("纯内存 Hub 投影异常")
	}
}

// TestHubDoneSyncsJournal Done → Complete(succeeded)；Progress 补满；重复调用幂等。
func TestHubDoneSyncsJournal(t *testing.T) {
	s, _ := newTestStore(t)
	h := NewHub(s)
	seedTxn(t, s, "tx-done", "markeron", TxnOpInstall)

	hd := h.Begin("markeron", extapi.OpInstall, "tx-done")
	snap := hd.Snapshot()
	if snap.Status != extapi.OpQueued || snap.Schema != extapi.ModuleContractSchema || !snap.Cancellable {
		t.Fatalf("Begin 载荷初值不对: %+v", snap)
	}
	if act := h.Active(); len(act) != 1 || act[0].ID != snap.ID {
		t.Fatalf("Active 未投影在途: %+v", act)
	}
	hd.Phase("unpack")
	p := 40
	hd.Progress(&p)
	running := hd.Snapshot()
	if running.Status != extapi.OpRunning || running.Phase != "unpack" || running.Progress == nil || *running.Progress != 40 {
		t.Fatalf("Phase/Progress 迁移不对: %+v", running)
	}
	if *(&p) != 40 {
		t.Error("Progress 与调用方内存共享了指针")
	}
	hd.Done()
	hd.Done() // 幂等：第二次无操作
	hd.Fail(&extapi.OperationError{Code: "too-late"})
	final := hd.Snapshot()
	if final.Status != extapi.OpSucceeded || final.Error != nil {
		t.Fatalf("终态幂等被破坏: %+v", final)
	}
	if len(h.Active()) != 0 {
		t.Error("收口后仍在 Active")
	}
	rec := h.Recent("markeron", 5)
	if len(rec) != 1 || rec[0].Status != extapi.OpSucceeded || rec[0].FinishedAt == "" {
		t.Fatalf("Recent 投影不对: %+v", rec)
	}
	if *rec[0].Progress != 100 {
		t.Errorf("Done 未补满进度: %+v", rec[0])
	}
	got, err := s.Get("tx-done")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != TxnSucceeded {
		t.Errorf("journal 未随 Done 收口: %s", got.State)
	}
}

// TestHubFailAndCancel Fail→journal(failed+error)，Cancel→journal(compensated 开放态)。
func TestHubFailAndCancel(t *testing.T) {
	s, _ := newTestStore(t)
	h := NewHub(s)
	seedTxn(t, s, "tx-fail", "rufus", TxnOpUpdate)
	seedTxn(t, s, "tx-cancel", "quicklook", TxnOpInstall)

	hd := h.Begin("rufus", extapi.OpUpdate, "tx-fail")
	hd.Fail(&extapi.OperationError{Code: "download-timeout", Message: "上游超时", Recoverable: true})
	if snap := hd.Snapshot(); snap.Status != extapi.OpFailed || snap.Error == nil || snap.Error.Code != "download-timeout" {
		t.Fatalf("Fail 载荷不对: %+v", snap)
	}
	got, _ := s.Get("tx-fail")
	if got.State != TxnFailed || got.Error == nil || got.Error.Code != "download-timeout" {
		t.Errorf("journal 未随 Fail 落账: %+v", got)
	}

	hd2 := h.Begin("quicklook", extapi.OpInstall, "tx-cancel")
	hd2.Cancel()
	if snap := hd2.Snapshot(); snap.Status != extapi.OpCancelled {
		t.Fatalf("Cancel 载荷不对: %+v", snap)
	}
	got2, _ := s.Get("tx-cancel")
	if got2.State != TxnCompensated {
		t.Errorf("journal 未以 compensated 落账: %s", got2.State)
	}
	live, _ := s.Pending()
	found := false
	for _, j := range live {
		if j.TransactionID == "tx-cancel" {
			found = true
		}
	}
	if !found {
		t.Error("compensated 应留在 Pending 等待闭环")
	}
}

// TestHubNonJournaledKinds invoke/activate/stop 不进恢复面：传入 txnID 被忽略，
// 终态不碰 journal（Wave 0 备忘 2 的代码结构强制）。
func TestHubNonJournaledKinds(t *testing.T) {
	s, _ := newTestStore(t)
	h := NewHub(s)
	seedTxn(t, s, "tx-lease", "markeron", TxnOpInstall)

	hd := h.Begin("markeron", extapi.OpInvoke, "tx-lease")
	hd.Done()
	got, _ := s.Get("tx-lease")
	if got.State != TxnPending {
		t.Errorf("invoke 租约终态误触 journal: %s", got.State)
	}
	if snap := hd.Snapshot(); snap.Status != extapi.OpSucceeded {
		t.Errorf("invoke 观察面本身不对: %+v", snap)
	}
}

// TestHubJournalSyncDegrades 托管事务账本被旁路删除时：观察面如实降级为
// journal-sync-failed，但绝不伪装成功；下次回灌仍按 Pending 兜底。
func TestHubJournalSyncDegrades(t *testing.T) {
	s, dir := newTestStore(t)
	seedTxn(t, s, "tx-lost", "markeron", TxnOpInstall)
	h := NewHub(s)
	hd := h.Begin("markeron", extapi.OpInstall, "tx-lost")
	if hd.Snapshot().Error != nil {
		t.Fatal("初值应无错误")
	}
	if err := os.Remove(filepath.Join(dir, "tx-lost.json")); err != nil {
		t.Fatal(err)
	}
	hd.Done()
	snap := hd.Snapshot()
	if snap.Status != extapi.OpSucceeded || snap.Error == nil || snap.Error.Code != "journal-sync-failed" {
		t.Fatalf("降级未如实记录: %+v", snap)
	}
}

// TestHubRecentProjection Recent 合并回灌/在途/终态，按 StartedAt 倒序 + n 限长。
func TestHubRecentProjection(t *testing.T) {
	s, _ := newTestStore(t)
	seedTxn(t, s, "tx-old", "markeron", TxnOpUpdate) // 回灌：createdAt=2026-09-01
	h := NewHub(s)
	hd := h.Begin("markeron", extapi.OpInstall, "")
	hd.Done()
	ids := []string{}
	for _, op := range h.Recent("markeron", 0) {
		if !strings.HasPrefix(op.ID, "resumed-tx-old") && !strings.HasPrefix(op.ID, "op-") {
			t.Errorf("意外记录 %s", op.ID)
		}
		ids = append(ids, op.ID)
	}
	if len(ids) != 2 || ids[0] != hd.Snapshot().ID {
		t.Errorf("倒序不对: %v", ids)
	}
	if len(h.Recent("markeron", 1)) != 1 {
		t.Error("n 限长失效")
	}
}

// TestHubConcurrent 并发登记/推进/收口与并发观察不破坏一致性（无 -race 环境下的
// 结构性防抖：全部迁移都在 hub.mu 下完成）。
func TestHubConcurrent(t *testing.T) {
	s, _ := newTestStore(t)
	h := NewHub(s)
	var wg sync.WaitGroup
	for i := range 32 {
		txn := fmt.Sprintf("txn-h%02d", i)
		module := fmt.Sprintf("mod%02d", i)
		seedTxn(t, s, txn, module, TxnOpInstall)
		wg.Add(1)
		go func() {
			defer wg.Done()
			hd := h.Begin(module, extapi.OpInstall, txn)
			hd.Phase("verify")
			p := 50
			hd.Progress(&p)
			hd.Done()
		}()
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = h.Active()
				_ = h.Recent("", 8)
			}
		}
	}()
	wg.Wait()
	close(done)
	if len(h.Active()) != 0 {
		t.Errorf("仍有未收口在途: %+v", h.Active())
	}
	rec := h.Recent("", 0)
	succeeded := 0
	for _, op := range rec {
		if op.Status == extapi.OpSucceeded {
			succeeded++
		}
	}
	if succeeded != 32 {
		t.Fatalf("收口计数异常: %d/32", succeeded)
	}
	// journal 侧全部随之收口。
	live, _ := s.Pending()
	if len(live) != 0 {
		t.Errorf("journal 未随 Done 收口: %v", live)
	}
}

// TestHubCancellableHonesty（P0 批 3·§5-1）：可取消标注仅授予批 2b 接通了
// 真取消链（ctx+租约）的资产安装/更新事务；invoke/activate 等无取消通道如实 false。
func TestHubCancellableHonesty(t *testing.T) {
	h := NewHub(nil)
	for _, tc := range []struct {
		kind extapi.OperationKind
		want bool
	}{
		{extapi.OpInstall, true},
		{extapi.OpUpdate, true},
		{extapi.OpRollback, false},
		{extapi.OpRemove, false},
		{extapi.OpRepair, false},
		{extapi.OpActivate, false},
	} {
		hd := h.Begin("m", tc.kind, "")
		if got := hd.Snapshot().Cancellable; got != tc.want {
			t.Errorf("kind %q cancellable = %v, want %v", tc.kind, got, tc.want)
		}
	}
}
