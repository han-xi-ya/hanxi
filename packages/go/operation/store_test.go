package operation

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hanxi/internal/extapi"
)

// newTestStore 建临时账本目录并返回 Store。
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s, dir
}

// testJournal 构造一笔合法的 install 事务（Begin 默认值之外全部显式给出）。
func testJournal(txn, module string) Journal {
	return Journal{
		Schema:        Schema,
		TransactionID: txn,
		Operation:     TxnOpInstall,
		DeliveryKind:  extapi.DeliveryManagedDeclarative,
		ModuleID:      module,
		ToVersion:     ptrString("1.0.0"),
		Phase:         "prepare",
		State:         TxnPending,
		CreatedAt:     "2026-09-01T00:00:00Z",
		UpdatedAt:     "2026-09-01T00:00:00Z",
		DataPolicy:    DataRetain,
	}
}

// TestJournalFrozenFieldNames 落盘键面逐字对齐 §8.3 的十五个字段，一个不多一个不少。
func TestJournalFrozenFieldNames(t *testing.T) {
	s, dir := newTestStore(t)
	if err := s.Begin(testJournal("txn-keys", "markeron")); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "txn-keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != len(journalFieldNames()) {
		t.Fatalf("字段数 %d != 15: %v", len(keys), keys)
	}
	for _, name := range journalFieldNames() {
		if _, ok := keys[name]; !ok {
			t.Errorf("缺字段 %q", name)
		}
	}
	// 样例值逐字核对：null 位的键必须存在（不省略）。
	if string(keys["fromVersion"]) != "null" || string(keys["activeBefore"]) != "null" || string(keys["error"]) != "null" {
		t.Errorf("可空字段未按样例落 null: %s %s %s", keys["fromVersion"], keys["activeBefore"], keys["error"])
	}
}

// TestBeginValidation Begin 的 schema/枚举/命名规范校验逐分支拒绝。
func TestBeginValidation(t *testing.T) {
	s, _ := newTestStore(t)
	cases := []struct {
		name string
		mut  func(j *Journal)
	}{
		{"schema", func(j *Journal) { j.Schema = 2 }},
		{"empty txn", func(j *Journal) { j.TransactionID = "" }},
		{"path txn", func(j *Journal) { j.TransactionID = "../evil" }},
		{"leading dot txn", func(j *Journal) { j.TransactionID = ".hidden" }},
		{"operation invoke 不进 journal", func(j *Journal) { j.Operation = "invoke" }},
		{"deliveryKind", func(j *Journal) { j.DeliveryKind = "community" }},
		{"state", func(j *Journal) { j.State = "processing" }},
		{"begin 终态", func(j *Journal) { j.State = TxnSucceeded }},
		{"begin 补偿态", func(j *Journal) { j.State = TxnCompensated }},
		{"dataPolicy", func(j *Journal) { j.DataPolicy = "whatever" }},
		{"phase 必填", func(j *Journal) { j.Phase = "" }},
		{"step 态", func(j *Journal) { j.Steps = []Step{{Name: "verify", State: "half-done"}} }},
		{"step 名", func(j *Journal) { j.Steps = []Step{{Name: " ", State: TxnPending}} }},
		{"时间戳", func(j *Journal) { j.CreatedAt = "昨天" }},
	}
	for _, tc := range cases {
		j := testJournal("txn-valid", "markeron")
		tc.mut(&j)
		if err := s.Begin(j); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: 期望 ErrValidation, 实得 %v", tc.name, err)
		}
	}
	// 合法值必须放行（对照组）。
	if err := s.Begin(testJournal("txn-valid", "markeron")); err != nil {
		t.Fatalf("合法 Begin 被拒: %v", err)
	}
}

// TestBeginDefaultsAndReopen Begin 补默认值、先落盘；换一个 Store 实例重开可读。
func TestBeginDefaultsAndReopen(t *testing.T) {
	s, dir := newTestStore(t)
	j := testJournal("txn-def", "markeron")
	j.Schema, j.State, j.CreatedAt, j.UpdatedAt, j.DataPolicy, j.Steps = 0, "", "", "", "", nil
	if err := s.Begin(j); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// 同目录开第二个 Store 实例（模拟进程重启）：必须能读到刚落盘的账。
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get("txn-def")
	if err != nil {
		t.Fatalf("重开 Get: %v", err)
	}
	if got.Schema != Schema || got.State != TxnPending || got.DataPolicy != DataRetain {
		t.Errorf("默认值未补齐: %+v", got)
	}
	if got.Steps == nil || len(got.Steps) != 0 {
		t.Errorf("Steps 应落为 []，实得 %#v", got.Steps)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("时间戳未补: %+v", got)
	}
}

// TestBeginSameModuleErrTxnActive 同 moduleId 未收口事务占坑（§8.2 单写事务）。
func TestBeginSameModuleErrTxnActive(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Begin(testJournal("txn-first", "markeron")); err != nil {
		t.Fatal(err)
	}
	// pending 占坑 → 拒绝
	if err := s.Begin(testJournal("txn-second", "markeron")); !errors.Is(err, ErrTxnActive) {
		t.Errorf("pending 冲突期望 ErrTxnActive, 实得 %v", err)
	}
	// running 同样占坑
	if err := s.Advance("txn-first", "download", []Step{{Name: "download", State: TxnRunning}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(testJournal("txn-second", "markeron")); !errors.Is(err, ErrTxnActive) {
		t.Errorf("running 冲突期望 ErrTxnActive, 实得 %v", err)
	}
	// 不同模块并行合法
	if err := s.Begin(testJournal("txn-other", "rufus")); err != nil {
		t.Errorf("跨模块应并行: %v", err)
	}
	// 收口后同模块可开新事务
	if err := s.Complete("txn-first", "succeeded", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(testJournal("txn-retry", "markeron")); err != nil {
		t.Errorf("收口后应放行新事务: %v", err)
	}
}

// TestBeginConcurrentSameModuleExactlyOne 并发 Begin 同模块：恰一笔成功，其余 ErrTxnActive。
func TestBeginConcurrentSameModuleExactlyOne(t *testing.T) {
	s, _ := newTestStore(t)
	const n = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ok, busy int
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.Begin(testJournal(txnName(i), "markeron"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTxnActive):
				busy++
			default:
				t.Errorf("并发 Begin 出现意外错误: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 || busy != n-1 {
		t.Fatalf("期望 1 成功 %d 冲突，实得 ok=%d busy=%d", n-1, ok, busy)
	}
}

func txnName(i int) string { return "txn-c" + string(rune('a'+i)) }

// TestAdvanceCrashWindow tmp+rename 窗口的崩溃安全：半途孤儿不伪装成事务、
// 目标账本要么旧版完好；孤儿可被下一次 Advance 的同名写入吸收。
func TestAdvanceCrashWindow(t *testing.T) {
	s, dir := newTestStore(t)
	if err := s.Begin(testJournal("txn-crash", "markeron")); err != nil {
		t.Fatal(err)
	}
	// 模拟"写入中途被 kill"：残留一个半成品 tmp（名字与 Store 的 tmp 约定一致）。
	stray := filepath.Join(dir, "txn-crash.json.tmp.4242")
	if err := os.WriteFile(stray, []byte(`{"schema":1,"transa`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 原账完好、照常可读；孤儿不被解析成事务。
	got, err := s.Get("txn-crash")
	if err != nil || got.State != TxnPending {
		t.Fatalf("孤儿 tmp 干扰了原账: %+v %v", got, err)
	}
	live, err := s.Pending()
	if err != nil || len(live) != 1 || live[0].TransactionID != "txn-crash" {
		t.Fatalf("Pending 被孤儿污染: %v %v", live, err)
	}
	// 正常推进不受影响（新 tmp 名按当前 pid，孤儿留待宿主清理）。
	if err := s.Advance("txn-crash", "verify", []Step{{Name: "verify", State: TxnRunning}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get("txn-crash")
	if got.State != TxnRunning || got.Phase != "verify" {
		t.Errorf("Advance 后状态不对: %+v", got)
	}
}

// TestAdvanceDerivation Advance 从 steps 自动派生 state 的四个分支 + phase 空串语义。
func TestAdvanceDerivation(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Begin(testJournal("txn-derive", "markeron")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		phase string
		steps []Step
		want  TxnState
	}{
		{"空 steps → pending", "", nil, TxnPending},
		{"有未完成 → running", "download", []Step{{Name: "dl", State: TxnSucceeded}, {Name: "unpack", State: TxnRunning}}, TxnRunning},
		{"含补偿中 step 仍算推进", "", []Step{{Name: "dl", State: TxnSucceeded}, {Name: "unpack", State: TxnCompensated}}, TxnRunning},
		{"任一失败 → failed", "verify", []Step{{Name: "dl", State: TxnSucceeded}, {Name: "verify", State: TxnFailed}}, TxnFailed},
	}
	wantPhase := "prepare" // 首个用例 phase 为空串 → 保持 Begin 落账的 prepare
	for _, tc := range cases {
		if err := s.Advance("txn-derive", tc.phase, tc.steps); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, err := s.Get("txn-derive")
		if err != nil {
			t.Fatal(err)
		}
		if got.State != tc.want {
			t.Errorf("%s: 派生 %s, 期望 %s", tc.name, got.State, tc.want)
		}
		if tc.phase != "" {
			wantPhase = tc.phase
		}
		if got.Phase != wantPhase {
			t.Errorf("%s: phase %q, 期望保持 %q", tc.name, got.Phase, wantPhase)
		}
	}
	// 派生 succeeded/failed 后账本进入终态语义的只有 succeeded/failed：
	// failed 派生后 Advance 拒绝（终态只读），Complete 仍可补写错误。
	if err := s.Advance("txn-derive", "x", nil); !errors.Is(err, ErrTxnClosed) {
		t.Errorf("failed 派生后再 Advance 期望 ErrTxnClosed, 实得 %v", err)
	}
	e := &extapi.OperationError{Code: "unpack-failed", Message: "解包校验失败"}
	if err := s.Complete("txn-derive", "failed", e); err != nil {
		t.Fatalf("failed 派生现场补写错误: %v", err)
	}
	got, _ := s.Get("txn-derive")
	if got.Error == nil || got.Error.Code != "unpack-failed" {
		t.Errorf("opErr 未落账: %+v", got.Error)
	}
}

// TestAdvanceAllSucceededCompletes 全部步骤 succeeded → 派生 succeeded，自动出 Pending。
func TestAdvanceAllSucceededCompletes(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Begin(testJournal("txn-all", "markeron")); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("txn-all", "commit", []Step{{Name: "verify", State: TxnSucceeded}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("txn-all")
	if got.State != TxnSucceeded {
		t.Fatalf("期望派生 succeeded, 实得 %s", got.State)
	}
	live, err := s.Pending()
	if err != nil || len(live) != 0 {
		t.Fatalf("派生终态后仍被 Pending 收编: %v %v", live, err)
	}
}

// TestAdvanceGuards 身份字段不可变 + 迁移结果仍须过 schema 校验。
func TestAdvanceGuards(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Begin(testJournal("txn-guard", "markeron")); err != nil {
		t.Fatal(err)
	}
	// steps 快照含非法态 → 校验拒绝，旧版完好。
	before, _ := s.Get("txn-guard")
	if err := s.Advance("txn-guard", "verify", []Step{{Name: "dl", State: "half-done"}}); !errors.Is(err, ErrValidation) {
		t.Errorf("非法 step 态期望 ErrValidation, 实得 %v", err)
	}
	after, _ := s.Get("txn-guard")
	if after.State != before.State || after.Phase != before.Phase {
		t.Errorf("校验失败却改动了账本: %+v", after)
	}
	// 未知事务。
	if err := s.Advance("no-such-txn", "p", nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 fs.ErrNotExist, 实得 %v", err)
	}
	// 不安全 ID 直接拒。
	if err := s.Advance("bad name", "p", nil); !errors.Is(err, ErrValidation) {
		t.Errorf("期望 ErrValidation, 实得 %v", err)
	}
}

// TestCompleteStates Complete 三收口态、failed 强制 opErr、非 failed 拒绝 opErr、
// compensated 可继续 Advance、同态幂等、终态拒绝翻案。
func TestCompleteStates(t *testing.T) {
	s, _ := newTestStore(t)

	// succeeded 收口。
	if err := s.Begin(testJournal("txn-s", "markeron")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-s", "succeeded", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-s", "succeeded", nil); err != nil {
		t.Errorf("同态 Complete 应幂等: %v", err)
	}
	if err := s.Complete("txn-s", "failed", &extapi.OperationError{Code: "x"}); !errors.Is(err, ErrTxnClosed) {
		t.Errorf("终态翻案期望 ErrTxnClosed, 实得 %v", err)
	}
	if err := s.Advance("txn-s", "p", nil); !errors.Is(err, ErrTxnClosed) {
		t.Errorf("终态 Advance 期望 ErrTxnClosed, 实得 %v", err)
	}

	// failed 强制 opErr。
	if err := s.Begin(testJournal("txn-f", "rufus")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-f", "failed", nil); !errors.Is(err, ErrValidation) {
		t.Errorf("failed 缺 opErr 期望 ErrValidation, 实得 %v", err)
	}
	if err := s.Complete("txn-f", "failed", &extapi.OperationError{Code: "net", Message: "下载超时"}); err != nil {
		t.Fatal(err)
	}

	// 非 failed 拒绝 opErr。
	if err := s.Begin(testJournal("txn-e", "quicklook")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-e", "succeeded", &extapi.OperationError{Code: "net"}); !errors.Is(err, ErrValidation) {
		t.Errorf("succeeded 带 opErr 期望 ErrValidation, 实得 %v", err)
	}

	// compensated：可收口、仍在 Pending、仍可 Advance 继续落账。
	if err := s.Begin(testJournal("txn-c", "vscode")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-c", "compensated", nil); err != nil {
		t.Fatal(err)
	}
	live, _ := s.Pending()
	found := false
	for _, j := range live {
		if j.TransactionID == "txn-c" && j.State == TxnCompensated {
			found = true
		}
	}
	if !found {
		t.Errorf("compensated 应在 Pending 中: %v", live)
	}
	if err := s.Advance("txn-c", "cleanup", []Step{{Name: "cleanup", State: TxnSucceeded}}); err != nil {
		t.Fatalf("compensated 后应可继续 Advance: %v", err)
	}
	if err := s.Complete("txn-c", "succeeded", nil); err != nil {
		t.Fatalf("补偿现场最终落账: %v", err)
	}
	if err := s.Begin(testJournal("txn-c2", "vscode")); err != nil {
		t.Errorf("compensated 事务不应阻塞同模块新事务: %v", err)
	}

	// 非法收口态。
	if err := s.Complete("txn-c2", "running", nil); !errors.Is(err, ErrValidation) {
		t.Errorf("期望 ErrValidation, 实得 %v", err)
	}
}

// TestPendingOrder Pending 按 (createdAt, transactionId) 稳定排序，重复读取一致。
func TestPendingOrder(t *testing.T) {
	s, _ := newTestStore(t)
	seed := []struct {
		txn, module, created string
	}{
		{"txn-b", "modb", "2026-09-02T00:00:00Z"},
		{"txn-a", "moda", "2026-09-01T00:00:00Z"},
		{"txn-c", "modc", "2026-09-01T00:00:00Z"}, // 同 createdAt，按 txnID 决胜
	}
	for _, x := range seed {
		j := testJournal(x.txn, x.module)
		j.CreatedAt, j.UpdatedAt = x.created, x.created
		j.State = TxnRunning
		if err := s.Begin(j); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 { // 重复扫描顺序一致（稳定排序）
		live, err := s.Pending()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, j := range live {
			names = append(names, j.TransactionID)
		}
		if strings.Join(names, ",") != "txn-a,txn-c,txn-b" {
			t.Fatalf("顺序不稳: %v", names)
		}
	}
}

// TestAbandonedDirs 只认目录、按前缀分桶，孤儿判定尊重未收口事务背书。
func TestAbandonedDirs(t *testing.T) {
	s, dir := newTestStore(t)
	// 未收口事务背书的目录不是孤儿；已收口/无账的目录是孤儿。
	if err := s.Begin(testJournal("txn-live", "markeron")); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(testJournal("txn-done", "rufus")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete("txn-done", "succeeded", nil); err != nil {
		t.Fatal(err)
	}
	mk := func(names ...string) {
		for _, n := range names {
			if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk(".installing-txn-live", ".installing-txn-done", ".installing-orphan",
		".removing-gone", ".tmp-stage", ".tmp-txn-live")
	// 同名普通文件不算目录。
	if err := os.WriteFile(filepath.Join(dir, ".installing-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst, remo, stag, err := s.AbandonedDirs()
	if err != nil {
		t.Fatal(err)
	}
	want := func(got []string, names ...string) bool {
		if len(got) != len(names) {
			return false
		}
		for i, n := range names {
			if filepath.Base(got[i]) != n {
				return false
			}
		}
		return true
	}
	if !want(inst, ".installing-orphan", ".installing-txn-done") {
		t.Errorf("installing 桶不对: %v", inst)
	}
	if !want(remo, ".removing-gone") {
		t.Errorf("removing 桶不对: %v", remo)
	}
	if !want(stag, ".tmp-stage") {
		t.Errorf("staging 桶不对: %v", stag)
	}
}

// TestGetNotFoundAndUnsafeID Get 的错误口径。
func TestGetNotFoundAndUnsafeID(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Get("no-exist"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 fs.ErrNotExist, 实得 %v", err)
	}
	if _, err := s.Get(`..\evil`); !errors.Is(err, ErrValidation) {
		t.Errorf("期望 ErrValidation, 实得 %v", err)
	}
	if err := s.Complete("no-exist", "succeeded", nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Complete 缺失期望 fs.ErrNotExist, 实得 %v", err)
	}
}
