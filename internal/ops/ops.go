// Package ops 是 Wave 4 共享托管内核 operation（journal 账本 + 在途操作观察面）
// 在装配根与各托管模块之间的接线层：
//
//   - 装配根（internal/app）在模块构造前经 SetKernel 注入账本与观察面对象；
//     未注入（单测/降级场景）时 BeginTxn 返回的 Txn 全部方法安全退化为 no-op，
//     模块主流程不受账本可用性影响；
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
	kernelMu sync.RWMutex
	kern     kernel
)

// SetKernel 注入 journal 账本与观察面 Hub（任一可为 nil：无账模式/纯观察模式）。
// 由装配根在模块构造前调用；重复调用以最后一次为准（仅装配期语义）。
func SetKernel(store *operation.Store, hub *operation.Hub) {
	kernelMu.Lock()
	defer kernelMu.Unlock()
	kern = kernel{store: store, hub: hub}
}

// Kernel 返回当前注入的内核引用（未注入时两值均为 nil）。
func Kernel() (*operation.Store, *operation.Hub) {
	kernelMu.RLock()
	defer kernelMu.RUnlock()
	return kern.store, kern.hub
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
	t := &Txn{txnID: txnID}
	store, hub := Kernel()
	if store == nil && hub == nil {
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
// phase 改为本步名。越界忽略。journal 每步迁移即持久化（§8.2）；
// 记账写盘失败只告警不中断主流程（内存投影与 Handle 照常，最终收口时
// Handle 的 journal-sync 失败会如实落在载荷 error 里）。
func (t *Txn) Step(i int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.steps == nil || i < 0 || i >= len(t.steps) {
		return
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
	t.advanceLocked()
	if t.handle != nil {
		t.handle.Phase(t.phase)
	}
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
func (t *Txn) advanceLocked() {
	if t.store == nil {
		return
	}
	if err := t.store.Advance(t.txnID, t.phase, t.steps); err != nil {
		slog.Warn("ops: 事务步进记账失败（不中断主流程）", "txn", t.txnID, "phase", t.phase, "err", err)
	}
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
