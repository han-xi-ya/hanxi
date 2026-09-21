// Package ops 是 Wave 4 共享托管内核 operation（journal 账本 + 在途操作观察面）
// 在装配根与各托管模块之间的接线层：
//
//   - 装配根（internal/app）在模块构造前经 SetKernel 注入账本与观察面对象；
//     从未注入（单测/无账头less 场景）时 BeginTxn 返回的 Txn 全部方法安全
//     退化为 no-op，模块主流程不受账本可用性影响；
//   - 账本 fail-closed 闸门（P0 批 2a，审查 §3.4）：装配期 OpenStore/Recover
//     失败经 MarkJournalDegraded 挂"降级"标记，运行期任何一次步进记账失败
//     同样置位（磁盘已不可信）——此后 BeginTxn 一律拒绝新开资产写事务
//     （"无账执行"被焊死），当前在途事务就地按 journal-degraded 收口上报；
//     副作用的即时中断依赖事务生命周期机制（批 2b），本批先保证"不新增裸奔、
//     已裸奔如实可见"；前端降级横幅属批 3 观察面接线，健康查询走
//     AppService.GetJournalHealth。
//   - 托管模块（markeron/rufus 起步）在下载/安装入口经 BeginTxn 开一笔持久化
//     事务：journal 先落盘再副作用（§8.2），过程步进经 Advance 记账，
//     收口经观察面 Handle.Done/Fail 自动同步落账（K-C 同步链路）；
//   - operation:changed 无载荷事件在事务登记与全部收口点（Begin/Done/Fail/
//     Cancel 等价位置）广播，作为前端"操作观察面有更新，请重拉"的触发信号；
//     既有模块各自的进度事件（<module>:version-download）不动，双通道并行。
//
// 本包零框架依赖（wails 仅经 application.Get 软引用广播事件），不采集任何
// 敏感信息入 journal（error.message 由调用方保证已脱敏）。
package ops

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/packages/go/operation"
)

// 事务内核引用：装配根启动期一次性注入（业务开放前），此后只读。
type kernel struct {
	store *operation.Store
	hub   *operation.Hub
}

var (
	kernelMu  sync.RWMutex
	kern      kernel
	kernelSet bool  // SetKernel 是否被调用过（区分"无账设计模式"与"装配后降级"）
	degraded  error // 账本降级原因（粘性：装配失败或运行期记账失败置位，SetKernel 复位）
)

// SetKernel 注入 journal 账本与观察面 Hub（任一可为 nil：无账模式/纯观察模式）。
// 由装配根在模块构造前调用；重复调用以最后一次为准（仅装配期语义），并复位
// 降级标记（装配根若在 MarkJournalDegraded 语义下重建内核 = 明示"账本已恢复"）。
func SetKernel(store *operation.Store, hub *operation.Hub) {
	kernelMu.Lock()
	defer kernelMu.Unlock()
	kern = kernel{store: store, hub: hub}
	kernelSet = true
	degraded = nil
}

// MarkJournalDegraded 挂账本降级标记（幂等，首因留档）：装配期 OpenStore/
// Recover 失败由装配根调用；运行期步进记账失败由 Txn 内部自动调用。
func MarkJournalDegraded(reason error) {
	if reason == nil {
		return
	}
	kernelMu.Lock()
	if degraded == nil {
		degraded = reason
		slog.Warn("ops: journal 账本降级，后续托管写事务将被拒绝（重启恢复）", "reason", reason)
	}
	kernelMu.Unlock()
}

// JournalDegraded 返回当前账本降级原因（nil = 健康）。
func JournalDegraded() error {
	kernelMu.RLock()
	defer kernelMu.RUnlock()
	return degraded
}

// Kernel 返回当前注入的内核引用（未注入时两值均为 nil）。
func Kernel() (*operation.Store, *operation.Hub) {
	kernelMu.RLock()
	defer kernelMu.RUnlock()
	return kern.store, kern.hub
}

// kernelLoaded 报告本进程是否显式注入过内核（SetKernel）。
func kernelLoaded() bool {
	kernelMu.RLock()
	defer kernelMu.RUnlock()
	return kernelSet
}

// BroadcastChanged 广播 operation:changed（Void）：观察面发生登记/收口类
// 变化时调用，提示前端重拉 ListOperations。Wails 未初始化（单测/装配早期）
// 静默跳过。
func BroadcastChanged() {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("operation:changed")
	}
}

// BeginTxn 开一笔托管资产安装/更新事务：
//
//  1. txnID 为调用方生成的事务 ID（同时派生 artifact.Tree 的 staging 目录名
//     .tmp-<txnID>，供崩溃恢复按"背书"定位现场，见 CleanTxnResidue）；
//  2. store 在场时 journal Begin 先落盘（每个 moduleId 同时只允许一笔未收口
//     写事务，冲突返回 ErrTxnActive——调用方应如实上抛而非并发覆盖）；
//  3. hub 在场时登记观察面记录（携 txnID，终态 Done/Fail 自动同步落账）并广播
//     operation:changed。
//
// kind 仅接受 install/update（资产事务词汇），delivery 如实区分资产形态
// （managed-declarative）与逻辑凭据（builtin-logical）。未注入内核时返回
// 仅记录步骤进度的内存态 Txn（降级：模块照常干活，只是不记账不观察）。
func BeginTxn(moduleID string, kind extapi.OperationKind, delivery extapi.DeliveryKind, toVersion, txnID string, steps []string) (*Txn, error) {
	if reason := JournalDegraded(); reason != nil {
		return nil, fmt.Errorf("journal 账本当前不可用（%v），为保证操作可恢复性已拒绝新的托管写事务；请重启 Hanxi 恢复账本后重试", reason)
	}
	t := &Txn{txnID: txnID}
	store, hub := Kernel()
	if !kernelLoaded() {
		// 从未注入内核（单测/无账设计模式）：内存态 Txn，照常干活不记账。
		t.steps = makeSteps(steps, operation.TxnPending)
		return t, nil
	}
	t.store = store // 步进 Advance 直写账本；终态收口仍优先经 Handle 同步落账

	op, err := journalOperation(kind)
	if err != nil {
		return nil, err
	}
	if store != nil {
		if err := store.Begin(operation.Journal{
			TransactionID: txnID,
			Operation:     op,
			DeliveryKind:  delivery,
			ModuleID:      moduleID,
			ToVersion:     ptrString(toVersion),
			Phase:         "resolve",
		}); err != nil {
			return nil, fmt.Errorf("开启安装事务账本失败: %w", err)
		}
	}
	if hub != nil {
		hubTxnID := txnID
		if store == nil {
			hubTxnID = "" // 无账模式：观察面收口不反向落账
		}
		t.handle = hub.Begin(moduleID, kind, hubTxnID)
		BroadcastChanged()
	}
	t.steps = makeSteps(steps, operation.TxnPending)
	return t, nil
}

// Txn 一笔模块事务。并发纪律：内部 mu 串行记账；Handle 自身幂等闩保证
// Done/Fail 只生效一次。未注入内核的降级 Txn 全方法 no-op。
type Txn struct {
	mu     sync.Mutex
	txnID  string
	store  *operation.Store // 仅"有账无观察面"降级路径直用
	handle *operation.Handle
	steps  []operation.Step
	phase  string
}

// TxnID 返回事务 ID（调用方命名 staging 等事务现场用）。
func (t *Txn) TxnID() string {
	if t == nil {
		return ""
	}
	return t.txnID
}

// Step 推进到第 i 步（0 起）：之前的步骤记 succeeded、本步记 running，
// phase 改为本步名。越界忽略。journal 每步迁移即持久化（§8.2）。
//
// 记账失败 fail-closed（P0 批 2a，审查 §3.4）：置全场降级标记（后续新事务
// 一律被拒）+ 本事务就地按 journal-degraded 收口上报观察面，并返回错误供
// 调用方提前终止；已在途的副作用由批 2b 的事务生命周期机制负责中断。
// 既有模块闭包可暂不消费返回值（收口已由本包代发），批 2b 统一接线提前退出。
func (t *Txn) Step(i int) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.steps == nil || i < 0 || i >= len(t.steps) {
		return nil
	}
	for j := range t.steps {
		switch {
		case j < i:
			t.steps[j].State = operation.TxnSucceeded
		case j == i:
			t.steps[j].State = operation.TxnRunning
		default:
			t.steps[j].State = operation.TxnPending
		}
	}
	t.phase = t.steps[i].Name
	if err := t.advanceLocked(); err != nil {
		t.degradeLocked(err)
		return err
	}
	if t.handle != nil {
		t.handle.Phase(t.phase)
	}
	return nil
}

// Progress 上报量化进度（0-100 钳制由 Handle 负责）。仅内存投影，不写
// journal——下载流每块回调频繁，账本只在步骤迁移处落盘。
func (t *Txn) Progress(done, total int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.handle == nil || t.steps == nil {
		return
	}
	if total <= 0 || done < 0 {
		t.handle.Progress(nil)
		return
	}
	pct := int(done * 100 / total)
	t.handle.Progress(&pct)
}

// Done 成功收口：全部步骤记 succeeded、Advance 后由 Handle.Done 把 journal
// Complete(succeeded) 同步落账（无观察面时直用 store），广播 operation:changed。
// 幂等：重复调用静默。
func (t *Txn) Done() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.handle == nil && t.store == nil {
		return // 已收口或未开账
	}
	for i := range t.steps {
		t.steps[i].State = operation.TxnSucceeded
	}
	t.phase = "done"
	t.advanceLocked()
	if t.handle != nil {
		h := t.handle
		t.handle = nil
		t.store = nil
		h.Done()
	} else {
		s := t.store
		t.store = nil
		if err := s.Complete(t.txnID, string(operation.TxnSucceeded), nil); err != nil {
			slog.Warn("ops: 事务收口落账失败", "txn", t.txnID, "err", err)
		}
	}
	BroadcastChanged()
}

// Fail 失败收口：journal Complete(failed)+error（message 由调用方保证脱敏），
// 观察面载荷携带 opErr，广播 operation:changed。幂等：重复调用静默。
func (t *Txn) Fail(code, message string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.handle == nil && t.store == nil {
		return
	}
	for i := range t.steps {
		if t.steps[i].State == operation.TxnRunning {
			t.steps[i].State = operation.TxnFailed
		}
	}
	opErr := &extapi.OperationError{Code: code, Message: message, Recoverable: true}
	t.advanceLocked()
	if t.handle != nil {
		h := t.handle
		t.handle = nil
		t.store = nil
		h.Fail(opErr)
	} else {
		s := t.store
		t.store = nil
		if err := s.Complete(t.txnID, string(operation.TxnFailed), opErr); err != nil {
			slog.Warn("ops: 事务失败落账失败", "txn", t.txnID, "err", err)
		}
	}
	BroadcastChanged()
}

// advanceLocked 把当前步骤快照写进 journal（须持 t.mu；无账模式 no-op）。
// 返回写盘错误供 Step 决策 fail-closed；Done/Fail 收口路径维持"告警不中断"
// （彼时事务已在终态，报错无处可退，如实留痕）。
func (t *Txn) advanceLocked() error {
	if t.store == nil {
		return nil
	}
	if err := t.store.Advance(t.txnID, t.phase, t.steps); err != nil {
		return err
	}
	return nil
}

// degradeLocked 步进记账失败的就地收口（须持 t.mu）：全场挂降级牌 +
// 本事务以 journal-degraded 终态上报（Handle 幂等闩保证不与后续 Done/Fail
// 互相覆盖），广播触发前端重拉。
func (t *Txn) degradeLocked(cause error) {
	MarkJournalDegraded(fmt.Errorf("事务 %s 在阶段 %s 步进记账失败: %w", t.txnID, t.phase, cause))
	opErr := &extapi.OperationError{
		Code:        "journal-degraded",
		Message:     fmt.Sprintf("操作账本写入失败（%v）；本次操作已被记为失败并冻结新事务，已产生的文件变更请按界面提示核对", cause),
		Recoverable: true,
	}
	for i := range t.steps {
		if t.steps[i].State == operation.TxnRunning {
			t.steps[i].State = operation.TxnFailed
		}
	}
	if t.handle != nil {
		h := t.handle
		t.handle = nil
		t.store = nil
		h.Fail(opErr)
	} else if t.store != nil {
		s := t.store
		t.store = nil
		if err := s.Complete(t.txnID, string(operation.TxnFailed), opErr); err != nil {
			slog.Warn("ops: 降级收口落账同样失败", "txn", t.txnID, "err", err)
		}
	}
	BroadcastChanged()
}

// journalOperation 观察面 kind → journal 操作词汇（本包只承接资产安装/更新
// 两类持久化事务；其余 kind 拒绝，防误接非事务语义）。
func journalOperation(k extapi.OperationKind) (operation.TxnOperation, error) {
	switch k {
	case extapi.OpInstall:
		return operation.TxnOpInstall, nil
	case extapi.OpUpdate:
		return operation.TxnOpUpdate, nil
	default:
		return "", fmt.Errorf("ops: 不支持的事务类型 %q（仅 install/update）", k)
	}
}

func makeSteps(names []string, state operation.StepState) []operation.Step {
	steps := make([]operation.Step, 0, len(names))
	for _, n := range names {
		steps = append(steps, operation.Step{Name: n, State: state})
	}
	return steps
}

func ptrString(s string) *string { return &s }
