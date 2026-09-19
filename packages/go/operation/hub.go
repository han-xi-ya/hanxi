package operation

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/extapi"
)

// hubRecentCapacity 是 Hub 内存中保留的操作记录上限：终态记录按插入顺序从最旧
// 开始淘汰，未收口记录永不淘汰（在途操作观察面不许"丢现场"）。
// invoke 类高频短租约因此有界，不会把账撑爆。
const hubRecentCapacity = 256

// opSeq 是跨 Hub 单调递增的载荷 ID 序列（同进程唯一，配合纳秒时间戳防撞）。
var opSeq atomic.Uint64

// Hub 是在途操作观察面：Phase 1-2 逻辑事务与 Wave 4 起托管事务共用，载荷即
// extapi.Operation（Wave 0 冻结契约），供模块中心详情、首页最近任务与诊断视图
// 消费。纯内存实现，零后台 goroutine；锁纪律为 hub.mu → store.mu 单向嵌套
// （Store 永不回调 Hub）。
//
// 刷新恢复（§2.3）：前端不依赖组件内存——NewHub 时把 Store 中的未收口事务
// （含 compensated）回灌为 failed + error.code="resumable" + recoverable=true
// 的合成记录（kind 由 journal operation 派生：install/update/rollback/
// uninstall→remove/repair）。宿主在 Recover 收口后应重建/刷新 Hub 观察面，
// 使合成记录让位于真实补偿进度。
type Hub struct {
	store *Store

	mu       sync.Mutex
	live     []*Handle          // 插入序；含未收口与近期终态
	refilled []extapi.Operation // NewHub 回灌的历史未收口事务合成记录
}

// NewHub 构造观察面。store 传 nil 得到纯内存 Hub（不做回灌、终态不写 journal，
// 服务 Phase 1-2 逻辑事务）；带 store 时启动即回灌 Store.Pending()。
// 回灌读取失败只放弃回灌展示，不影响 Hub 可用性（诊断面而非事实源）。
func NewHub(store *Store) *Hub {
	h := &Hub{store: store}
	if store == nil {
		return h
	}
	pending, err := store.Pending()
	if err != nil {
		return h
	}
	for _, j := range pending {
		if op, ok := refilledOperation(j); ok {
			h.refilled = append(h.refilled, op)
		}
	}
	return h
}

// Begin 登记一笔操作。moduleID 为载荷展示用；非 nil 语义上应传 Catalog 规范 ID。
// kind ∈ {install, update, rollback, remove, repair} 之外（activate/stop/invoke）
// 时 txnID 必须为 ""，传入也会被忽略——持久化范围排除 invoke 是 Wave 0 备忘 2
// 的裁决，journal 词汇（§8.3）同样没有 stop/activate 的位置，由
// journalOperation 的代码结构强制。
//
// txnID 非空表示该操作背后已有一笔由调用方 Store.Begin 落盘的托管事务，
// Handle 终态迁移将同步收口该 journal；Hub 不代为创建 journal，Begin 本身
// 不落盘。
func (h *Hub) Begin(moduleID string, kind extapi.OperationKind, txnID string) *Handle {
	journalTxn := ""
	if _, ok := journalOperation(kind); ok {
		journalTxn = txnID
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	hd := &Handle{
		hub: h,
		op: extapi.Operation{
			Schema:      extapi.ModuleContractSchema,
			ID:          fmt.Sprintf("op-%d-%d", time.Now().UnixNano(), opSeq.Add(1)),
			TxnID:       journalTxn, // 托管事务裸 ID；非托管（invoke/activate 等）为空
			ModuleID:    moduleID,
			Kind:        kind,
			Status:      extapi.OpQueued,
			Cancellable: true,
			StartedAt:   nowRFC3339(),
		},
		txnID: journalTxn,
	}
	h.live = append(h.live, hd)
	for len(h.live) > hubRecentCapacity {
		if i := slices.IndexFunc(h.live, func(x *Handle) bool { return x.terminal() }); i >= 0 {
			h.live = slices.Delete(h.live, i, i+1)
			continue
		}
		break
	}
	return hd
}

// Active 返回未收口（queued/running）操作的载荷快照，按登记顺序。
func (h *Hub) Active() []extapi.Operation {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []extapi.Operation
	for _, hd := range h.live {
		if !hd.terminal() {
			out = append(out, hd.op)
		}
	}
	return out
}

// ForgetResumable 从回灌合成记录中摘除指定事务的 resumable 投影。宿主在
// 代用户把该事务收口（如 DismissResumable 以 failed 落账）后调用，使观察面
// 不再呈现"等待启动恢复处理"的过期合成记录；journal 账本本身的收口归
// Store.Complete，本方法只动内存投影。返回是否确有对应记录被摘除。
func (h *Hub) ForgetResumable(txnID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i := slices.IndexFunc(h.refilled, func(o extapi.Operation) bool { return o.TxnID == txnID }); i >= 0 {
		h.refilled = slices.Delete(h.refilled, i, i+1)
		return true
	}
	return false
}

// Recent 返回某模块的近期操作载荷快照（含回灌的合成记录），按 StartedAt 倒序，
// 最多 n 条（n<=0 表示不限量）；moduleID 传 "" 表示全模块。
func (h *Hub) Recent(moduleID string, n int) []extapi.Operation {
	h.mu.Lock()
	defer h.mu.Unlock()
	var pool []extapi.Operation
	for _, hd := range h.live {
		if moduleID == "" || hd.op.ModuleID == moduleID {
			pool = append(pool, hd.op)
		}
	}
	for _, op := range h.refilled {
		if moduleID == "" || op.ModuleID == moduleID {
			pool = append(pool, op)
		}
	}
	sort.SliceStable(pool, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339Nano, pool[i].StartedAt)
		tj, _ := time.Parse(time.RFC3339Nano, pool[j].StartedAt)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return pool[i].ID < pool[j].ID
	})
	if n > 0 && len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// ---------------------------------------------------------------------------
// Handle
// ---------------------------------------------------------------------------

// Handle 是一笔在途操作的登记句柄。全部方法可并发调用、重复调用幂等
// （第一次生效，后续调用静默无操作）；journal 同步只在终态发生一次。
type Handle struct {
	hub   *Hub
	op    extapi.Operation // guarded by hub.mu
	txnID string           // 非空=托管事务；终态经 Store.Complete 同步 journal
	done  bool             // 终态迁移已生效（幂等闩），guarded by hub.mu
}

// Phase 记录阶段名（任意可读词汇，与 journal phase 同一命名面）。
// queued 状态经首次 Phase/Progress 迁移为 running。
func (hd *Handle) Phase(phase string) {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if hd.done {
		return
	}
	switch hd.op.Status {
	case extapi.OpQueued:
		hd.op.Status = extapi.OpRunning
	case extapi.OpRunning:
	default: // 已收口载荷不再迁移
		return
	}
	hd.op.Phase = phase
}

// Progress 记录量化进度（0-100，越界钳制；传 nil 表示"不可量化"，清除现值）。
// queued 状态经首次 Phase/Progress 迁移为 running。
func (hd *Handle) Progress(pct *int) {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if hd.done || hd.op.Status == extapi.OpCancelled || hd.op.Status == extapi.OpFailed || hd.op.Status == extapi.OpSucceeded {
		return
	}
	var v *int
	if pct != nil {
		c := *pct
		if c < 0 {
			c = 0
		}
		if c > 100 {
			c = 100
		}
		v = &c
	}
	hd.op.Progress = v
	if hd.op.Status == extapi.OpQueued {
		hd.op.Status = extapi.OpRunning
	}
}

// Done 成功收口：载荷置 succeeded（Progress 有值则补满 100）；txnID 非空时
// journal 以 Complete(succeeded) 收口。步骤进度是引擎经 Store.Advance 记账的
// 现场，Handle 不代为改写（只收口，不伪造步骤账目）。
func (hd *Handle) Done() {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if hd.done {
		return
	}
	hd.done = true
	hd.op.Status = extapi.OpSucceeded
	hd.op.FinishedAt = nowRFC3339()
	if hd.op.Progress != nil {
		full := 100
		hd.op.Progress = &full
	}
	hd.syncJournalLocked(func(s *Store, txnID string) error {
		return s.Complete(txnID, string(TxnSucceeded), nil)
	})
}

// Fail 失败收口：载荷携带 opErr（调用方保证 message 已按 §8.2 脱敏；传 nil
// 则以通用 operation-failed 收口）；txnID 非空时 journal 以
// Complete(failed, opErr) 落账。
func (hd *Handle) Fail(opErr *extapi.OperationError) {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if hd.done {
		return
	}
	hd.done = true
	e := opErr
	if e == nil {
		e = &extapi.OperationError{Code: "operation-failed", Message: "操作失败（调用方未提供错误详情）"}
	} else {
		clone := *e
		e = &clone
	}
	hd.op.Status = extapi.OpFailed
	hd.op.FinishedAt = nowRFC3339()
	hd.op.Error = e
	hd.syncJournalLocked(func(s *Store, txnID string) error {
		return s.Complete(txnID, string(TxnFailed), e)
	})
}

// Cancel 取消收口：载荷置 cancelled；txnID 非空时 journal 以
// Complete(compensated, nil) 落账——取消意味着已补偿或补偿中，compensated
// 仍留在 Pending 里等恢复/后续落账，与 §8.8 的语义一致。
func (hd *Handle) Cancel() {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if hd.done {
		return
	}
	hd.done = true
	hd.op.Status = extapi.OpCancelled
	hd.op.FinishedAt = nowRFC3339()
	hd.syncJournalLocked(func(s *Store, txnID string) error {
		return s.Complete(txnID, string(TxnCompensated), nil)
	})
}

// Snapshot 返回载荷快照（诊断/测试友好；Active/Recent 已给出同类视图）。
func (hd *Handle) Snapshot() extapi.Operation {
	h := hd.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	return hd.op
}

// terminal 判定终态迁移是否已生效（guarded：调用方持 hub.mu）。
func (hd *Handle) terminal() bool { return hd.done }

// syncJournalLocked 在持有 hub.mu 时执行 journal 同步（锁序 hub.mu→store.mu）。
// 同步失败不回滚已生效的观察面收口（内存是投影，磁盘是账本）：journal 未能
// 收口时，下次启动的 Pending 会把它回灌为 resumable 呈现——降级路径与崩溃
// 恢复同构，如实记录在载荷 error 里（若尚未携带业务错误）。
func (hd *Handle) syncJournalLocked(sync func(s *Store, txnID string) error) {
	if hd.txnID == "" || hd.hub.store == nil {
		return
	}
	if err := sync(hd.hub.store, hd.txnID); err != nil {
		if hd.op.Error == nil {
			hd.op.Error = &extapi.OperationError{
				Code:        "journal-sync-failed",
				Message:     fmt.Sprintf("journal %s 同步失败（下次启动将以 resumable 回灌）: %v", hd.txnID, err),
				Recoverable: true,
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 载荷词汇 ↔ journal 词汇
// ---------------------------------------------------------------------------

// journalOperation 把观察面 kind 映射到 §8.3 journal operation 词汇；
// ok=false 表示该 kind 不进持久化面（invoke 为 Wave 0 备忘 2 的显式裁决，
// activate/stop 在 §8.3 枚举中不存在）。
func journalOperation(k extapi.OperationKind) (TxnOperation, bool) {
	switch k {
	case extapi.OpInstall:
		return TxnOpInstall, true
	case extapi.OpUpdate:
		return TxnOpUpdate, true
	case extapi.OpRollback:
		return TxnOpRollback, true
	case extapi.OpRemove:
		return TxnOpUninstall, true
	case extapi.OpRepair:
		return TxnOpRepair, true
	default:
		return "", false
	}
}

// operationKindOf 是 journalOperation 的逆映射（回灌/审计投影用）。
func operationKindOf(o TxnOperation) (extapi.OperationKind, bool) {
	switch o {
	case TxnOpInstall:
		return extapi.OpInstall, true
	case TxnOpUpdate:
		return extapi.OpUpdate, true
	case TxnOpRollback:
		return extapi.OpRollback, true
	case TxnOpUninstall:
		return extapi.OpRemove, true
	case TxnOpRepair:
		return extapi.OpRepair, true
	default:
		return "", false
	}
}

// refilledOperation 把一笔未收口事务（含 compensated）投影为刷新恢复合成载荷
// （§2.3：failed + resumable，不依赖组件内存）。
func refilledOperation(j Journal) (extapi.Operation, bool) {
	kind, ok := operationKindOf(j.Operation)
	if !ok {
		return extapi.Operation{}, false
	}
	return extapi.Operation{
		Schema:     extapi.ModuleContractSchema,
		ID:         "resumed-" + j.TransactionID, // 展示用合成键；收口通道消费 TxnID
		TxnID:      j.TransactionID,
		ModuleID:   j.ModuleID,
		Kind:       kind,
		Phase:      j.Phase,
		Status:     extapi.OpFailed,
		StartedAt:  j.CreatedAt,
		FinishedAt: j.UpdatedAt,
		Error: &extapi.OperationError{
			Code:        "resumable",
			Message:     fmt.Sprintf("上次进程未收口的托管事务（journal state=%s, phase=%s），等待启动恢复处理", j.State, j.Phase),
			Recoverable: true,
		},
	}, true
}
