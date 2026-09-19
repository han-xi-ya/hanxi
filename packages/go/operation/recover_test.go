package operation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// TestRecoverClassification §8.8 分拣矩阵：
// 接管→succeeded=Resumed、接管→failed/compensated=RolledBack、
// 无补偿器/补偿器报错/处理后未收口=Orphaned、已收口账目不进分派面。
func TestRecoverClassification(t *testing.T) {
	s, _ := newTestStore(t)
	seed := []struct {
		txn, module string
		op          TxnOperation
		state       TxnState
	}{
		{"tx-r1", "modaa", TxnOpInstall, TxnRunning},
		{"tx-r2", "modbb", TxnOpUpdate, TxnPending},
		{"tx-r3", "modcc", TxnOpRollback, TxnRunning},
		{"tx-r4", "moddd", TxnOpRepair, TxnPending},
		{"tx-r5", "modeee", TxnOpUninstall, TxnRunning},
		{"tx-done", "modff", TxnOpInstall, TxnRunning}, // 分派前已收口
	}
	for i, x := range seed {
		j := testJournal(x.txn, x.module)
		j.Operation, j.State = x.op, x.state
		j.CreatedAt = "2026-09-0" + string(rune('1'+i)) + "T00:00:00Z"
		if err := s.Begin(j); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Complete("tx-done", "succeeded", nil); err != nil {
		t.Fatal(err)
	}

	var dispatched []string
	report, err := Recover(s, func(j Journal) (bool, error) {
		dispatched = append(dispatched, j.TransactionID)
		switch j.TransactionID {
		case "tx-r1": // 接管并收口成功 → Resumed
			return true, s.Complete(j.TransactionID, "succeeded", nil)
		case "tx-r2": // 补偿失败落账 → RolledBack
			return true, s.Complete(j.TransactionID, "failed", &extapi.OperationError{Code: "unrecoverable", Message: "现场已无法对齐"})
		case "tx-r3": // 就地补偿完成 → RolledBack
			return true, s.Complete(j.TransactionID, "compensated", nil)
		case "tx-r4": // 未登记组合 → Orphaned
			return false, nil
		case "tx-r5": // 接管但没闭环 → Orphaned（仍未收口）
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}

	ids := func(list []Journal) string {
		parts := make([]string, 0, len(list))
		for _, j := range list {
			parts = append(parts, j.TransactionID)
		}
		return strings.Join(parts, ",")
	}
	if got := ids(report.Resumed); got != "tx-r1" {
		t.Errorf("Resumed=%q", got)
	}
	if got := ids(report.RolledBack); got != "tx-r2,tx-r3" {
		t.Errorf("RolledBack=%q（按 createdAt 稳定序）", got)
	}
	if got := ids(report.Orphaned); got != "tx-r4,tx-r5" {
		t.Errorf("Orphaned=%q", got)
	}
	// 分派面：已收口 tx-done 绝不出现在任何桶。
	for _, list := range [][]Journal{report.Resumed, report.RolledBack, report.Orphaned} {
		for _, j := range list {
			if j.TransactionID == "tx-done" {
				t.Fatal("已收口事务进入了恢复分派")
			}
		}
	}
	if len(dispatched) != 5 {
		t.Errorf("分派次数 %d != 5", len(dispatched))
	}
	// Orphaned 不自动执行：tx-r4 的磁盘账本原样未动。
	got, _ := s.Get("tx-r4")
	if got.State != TxnPending {
		t.Errorf("Orphaned 事务被改动了: %s", got.State)
	}
	// Orphaned 报告携带可读原因。
	r4 := report.Orphaned[0]
	if r4.Error == nil || r4.Error.Code != "compensation-unhandled" {
		t.Errorf("Orphaned 未带原因说明: %+v", r4.Error)
	}
	// compensated 仍是开放态：留在 Pending、可被下一轮恢复处理并最终闭环。
	live, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	r3Open := false
	for _, j := range live {
		if j.TransactionID == "tx-r3" && j.State == TxnCompensated {
			r3Open = true
		}
	}
	if !r3Open {
		t.Error("tx-r3 应以 compensated 留在 Pending")
	}
	if err := s.Advance("tx-r3", "cleanup", []Step{{Name: "cleanup", State: TxnSucceeded}}); err != nil {
		t.Errorf("compensated 事务应可继续 Advance: %v", err)
	}
}

// TestRecoverHandlerError 补偿器报错 → Orphaned（compensation-failed），账本不动。
func TestRecoverHandlerError(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Begin(testJournal("tx-err", "modaa")); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("补偿器炸了")
	report, err := Recover(s, func(j Journal) (bool, error) { return true, boom })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orphaned) != 1 || report.Orphaned[0].Error == nil ||
		report.Orphaned[0].Error.Code != "compensation-failed" {
		t.Fatalf("补偿器错误未如实上报: %+v", report)
	}
	got, _ := s.Get("tx-err")
	if got.State != TxnPending || got.Error != nil {
		t.Errorf("账本被越权改写: %+v", got)
	}
}

// TestRecoverOrderStable 分派顺序 = Pending 稳定序（createdAt→txnID），多轮一致。
func TestRecoverOrderStable(t *testing.T) {
	s, _ := newTestStore(t)
	seed := []struct{ txn, created string }{
		{"tx-b", "2026-09-02T00:00:00Z"},
		{"tx-a", "2026-09-01T00:00:00Z"},
		{"tx-c", "2026-09-01T00:00:00Z"},
	}
	for _, x := range seed {
		j := testJournal(x.txn, "mod"+strings.ToUpper(x.txn[3:]))
		j.CreatedAt, j.UpdatedAt = x.created, x.created
		if err := s.Begin(j); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		var order []string
		if _, err := Recover(s, func(j Journal) (bool, error) {
			order = append(order, j.TransactionID)
			return false, nil
		}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(order, ",") != "tx-a,tx-c,tx-b" {
			t.Fatalf("恢复顺序不稳: %v", order)
		}
	}
}

// TestRecoverQuarantine 解析失败/校验失败/键面夹带/文件名与 transactionId 不符的
// journal 一律隔离改名（绝不删除），计入 Quarantined；正常账不受影响。
func TestRecoverQuarantine(t *testing.T) {
	s, dir := newTestStore(t)
	if err := s.Begin(testJournal("tx-good", "modaa")); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(filepath.Join(dir, "tx-good.json"))
	if err != nil {
		t.Fatal(err)
	}
	corrupts := map[string]string{
		"tx-junk.json":     `{"schema":1,"trans`,                                                 // 解析失败
		"tx-schema.json":   `{"schema":9,"transactionId":"tx-schema","state":"pending"}`,         // 版本不符
		"tx-extra.json":    `{"schema":1,"transactionId":"tx-extra","secretCommandLine":"pw@1"}`, // 未知键（敏感旁路）
		"tx-mismatch.json": strings.Replace(string(good), "tx-good", "tx-sneaky", 1),             // 内容认领他人事务
	}
	for name, body := range corrupts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Recover(s, func(j Journal) (bool, error) { return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Quarantined) != len(corrupts) {
		var got []string
		for _, j := range report.Quarantined {
			got = append(got, j.TransactionID)
		}
		t.Fatalf("Quarantined=%v", got)
	}
	for _, q := range report.Quarantined {
		if q.Error == nil || q.Error.Code != "journal-corrupt" {
			t.Errorf("隔离条目缺取证说明: %+v", q)
		}
	}
	// 隔离 = 改名保留，不删任何字节。
	for name := range corrupts {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s 仍以 .json 名义在账", name)
		}
		if _, err := os.Stat(p + ".journal-corrupt"); err != nil {
			t.Errorf("%s 隔离文件丢失: %v", name, err)
		}
	}
	// tx-mismatch 的原始字节完整保留（取证面）。
	data, err := os.ReadFile(filepath.Join(dir, "tx-mismatch.json.journal-corrupt"))
	if err != nil {
		t.Fatal(err)
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil || j.TransactionID != "tx-sneaky" {
		t.Errorf("隔离内容被篡改: %v %+v", err, j)
	}
	// 正常账不受影响：tx-good 走的是 Orphaned（无补偿器），不是 Quarantined。
	if _, err := s.Get("tx-good"); err != nil {
		t.Errorf("tx-good 受损: %v", err)
	}
	// 再扫一轮：隔离件不再出现在任何分派面（.journal-corrupt 非 journal 账本）。
	live, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].TransactionID != "tx-good" {
		t.Fatalf("Pending 被隔离件污染: %v", live)
	}
}
