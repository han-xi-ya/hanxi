// Package operation 是官方模块化分发的共享托管内核之"持久化事务 + 崩溃恢复 +
// 在途操作观察面"（Wave 4，冻结依据：docs/plans/PLAN_OFFICIAL_MODULE_DISTRIBUTION.md §8）。
//
// # 职责边界
//
//   - 本包只管"记账"（journal 落盘、恢复分派、在途操作观察），不管资产目录：
//     下载/解包/版本目录/active pointer 归 packages/go/artifact，进程治理归
//     packages/go/supervisor；三者的交接面只有字符串约定——transactionId 与
//     目录名约定（.installing-<txn>/.removing-<txn>/.tmp-<txn>，见 §8.7）。
//     本包对目录只做"盘点"（Store.AbandonedDirs），不创建、不删除任何资产目录。
//   - 载荷契约与 internal/extapi/catalog.go 已冻结的 Operation 对齐：
//     观察面（Hub）直接输出 extapi.Operation（枚举 OpInstall/…/OpInvoke，状态机
//     queued/running/succeeded/failed/cancelled）；journal 落盘 schema 是 §8.3
//     专项词汇（install|update|rollback|uninstall|repair），两者差异仅在
//     uninstall↔remove 一个词上，映射收敛在 hub.go 的 journalOperation/operationKindOf。
//
// # 落盘纪律（§8.2，实现见 store.go/recover.go）
//
//   - journal 先落盘并 fsync，再执行副作用：Begin/Advance/Complete 的每次成功
//     返回都意味着内容已 Sync 到磁盘；调用方必须在其之后才做下载、解包、rename 等
//     有副作用的动作；
//   - 更新走 tmp+rename：中途崩溃只会留下 *.json.tmp.<pid> 孤儿，目标文件要么
//     旧版完好要么新版完好，不留半成品；
//   - 每一步状态迁移即持久化：引擎在每次 step/phase 迁移时调用 Advance；
//   - 每个 moduleId 同时只允许一个未收口写事务（Begin 以 ErrTxnActive 拒绝第二笔）；
//   - 崩溃恢复发生在模块启动之前：宿主启动早期调用 Recover，恢复收口前不得开放
//     业务调用（§8.8 第 6 条）；
//   - journal 不记录 Secret、用户文件内容或敏感完整命令行：字段面固定为 §8.3 的
//     十五个键（含 steps/error），error.message 由调用方保证已脱敏——本包只存
//     调用方写入的内容，不采集任何环境信息入 journal。
//
// # 持久化范围裁决（Wave 0 备忘 2）
//
// kind=invoke 是高频短租约（进程死即失效），不进 journal、不进恢复面；
// activate/stop 同理不在 §8.3 操作枚举内。Hub 对这三类 kind 一律不绑定事务 ID
// （Begin 传入的 txnID 被忽略），由代码结构强制排除。
package operation

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"hanxi/internal/extapi"
)

// Schema 是 journal 落盘 schema 的当前版本（§8.3 "schema": 1，冻结为 1）。
// 字段破坏性变更必须递增并同步 docs/adr/ 立案；§8.2 要求至少能读取上一个
// 稳定宿主产生的未完成事务，当前恒为 1，无兼容分支。
const Schema = 1

// ---------------------------------------------------------------------------
// 冻结枚举（§8.2/§8.3）
// ---------------------------------------------------------------------------

// TxnState 是事务与步骤共用的状态词汇："record every step
// pending/running/succeeded/failed/compensated"（§8.2）。
type TxnState string

const (
	TxnPending     TxnState = "pending"
	TxnRunning     TxnState = "running"
	TxnSucceeded   TxnState = "succeeded"
	TxnFailed      TxnState = "failed"
	TxnCompensated TxnState = "compensated"
)

// StepState 与 TxnState 同一取值域（§8.2 逐步记录口径）。
type StepState = TxnState

func (s TxnState) valid() bool {
	return slices.Contains([]TxnState{TxnPending, TxnRunning, TxnSucceeded, TxnFailed, TxnCompensated}, s)
}

// terminal 判定事务是否进入收口终态。只有 succeeded/failed 算终态：
// compensated 是"补偿已落账、等待闭环"的开放态——仍可 Advance 继续推进，
// 最终必须 Complete 到 succeeded/failed；在此之前它留在 Pending 里、
// 留在恢复分派面里（§8.8 崩溃恢复的续处理口径）。
func (s TxnState) terminal() bool {
	return s == TxnSucceeded || s == TxnFailed
}

// TxnOperation 是 journal 的事务类型词汇，逐字对齐 §8.3 专项枚举。
// 注意与 extapi.OperationKind 的差异（journal 用 uninstall，观察面用 remove；
// activate/stop/invoke 不进 journal），映射见 hub.go。
type TxnOperation string

const (
	TxnOpInstall   TxnOperation = "install"
	TxnOpUpdate    TxnOperation = "update"
	TxnOpRollback  TxnOperation = "rollback"
	TxnOpUninstall TxnOperation = "uninstall"
	TxnOpRepair    TxnOperation = "repair"
)

func (o TxnOperation) valid() bool {
	return slices.Contains([]TxnOperation{TxnOpInstall, TxnOpUpdate, TxnOpRollback, TxnOpUninstall, TxnOpRepair}, o)
}

// DeliveryKind 直接复用 extapi 已冻结的三值枚举（§8.1：同一事务框架服务
// builtin-logical / managed-declarative / official-sidecar 三种交付）。
type DeliveryKind = extapi.DeliveryKind

func deliveryKindValid(k extapi.DeliveryKind) bool {
	return slices.Contains([]extapi.DeliveryKind{
		extapi.DeliveryBuiltinLogical,
		extapi.DeliveryManagedDeclarative,
		extapi.DeliveryOfficialSidecar,
	}, k)
}

// DataPolicy 是事务携带的数据策略（§9.2 卸载确认的三档口径；安装/更新固定
// "retain"，§8.3 样例值）。
type DataPolicy string

const (
	DataRetain     DataPolicy = "retain"
	DataPurgeCache DataPolicy = "purge-cache"
	DataPurgeData  DataPolicy = "purge-data"
)

func (p DataPolicy) valid() bool {
	return slices.Contains([]DataPolicy{DataRetain, DataPurgeCache, DataPurgeData}, p)
}

// ---------------------------------------------------------------------------
// Journal 落盘 schema（§8.3 字段名逐字冻结）
// ---------------------------------------------------------------------------

// Step 是事务内的一个可补偿/幂等步骤。
type Step struct {
	Name  string    `json:"name"`
	State StepState `json:"state"`
}

// Journal 是一笔持久化事务的落盘记录，JSON 字段名与 §8.3 专项样例逐字一致：
//
//	schema/transactionId/operation/deliveryKind/moduleId/fromVersion/toVersion/
//	phase/state/createdAt/updatedAt/activeBefore/dataPolicy/steps/error
//
// 纪律声明（§8.2）：journal 不记录 Secret、用户文件内容或敏感完整命令行；
// Error.Message 由调用方负责在写入前脱敏，本类型刻意不提供自由 map 字段以
// 收窄误写面（未知键在校验中直接拒绝，防止敏感旁路入库）。
type Journal struct {
	Schema        int                    `json:"schema"`
	TransactionID string                 `json:"transactionId"`
	Operation     TxnOperation           `json:"operation"`
	DeliveryKind  DeliveryKind           `json:"deliveryKind"`
	ModuleID      string                 `json:"moduleId"`
	FromVersion   *string                `json:"fromVersion"` // null=无旧版本（如首装）
	ToVersion     *string                `json:"toVersion"`   // null=无目标版本（如卸载）
	Phase         string                 `json:"phase"`
	State         TxnState               `json:"state"`
	CreatedAt     string                 `json:"createdAt"`    // RFC3339
	UpdatedAt     string                 `json:"updatedAt"`    // RFC3339
	ActiveBefore  *string                `json:"activeBefore"` // null=事务前无激活版本
	DataPolicy    DataPolicy             `json:"dataPolicy"`
	Steps         []Step                 `json:"steps"`
	Error         *extapi.OperationError `json:"error"`
}

// 哨兵错误：校验失败统一可判。
var (
	// ErrValidation 表示字段违反 §8.3 schema/枚举/命名规范。
	ErrValidation = errors.New("operation: journal validation failed")
	// ErrCorrupt 表示磁盘文件存在但无法解析/校验（损坏或旁路写入）。
	ErrCorrupt = errors.New("operation: corrupt journal file")
)

// validate 按 §8.3 冻结面校验：schema、枚举、ID 命名规范、时间戳、步骤词汇、
// phase 非空。错误一律包装 ErrValidation，便于调用方 errors.Is 判定；
// 未知键的拒绝由反序列化入口 unmarshalStrict 负责。
func (j *Journal) validate() error {
	if j.Schema != Schema {
		return fmt.Errorf("%w: schema %d, want %d", ErrValidation, j.Schema, Schema)
	}
	if !isSafeID(j.TransactionID) {
		return fmt.Errorf("%w: transactionId %q must match ^[A-Za-z0-9][A-Za-z0-9._-]{0,62}[A-Za-z0-9]$", ErrValidation, j.TransactionID)
	}
	if !j.Operation.valid() {
		return fmt.Errorf("%w: operation %q not in install/update/rollback/uninstall/repair", ErrValidation, j.Operation)
	}
	if !deliveryKindValid(j.DeliveryKind) {
		return fmt.Errorf("%w: deliveryKind %q not in builtin-logical/managed-declarative/official-sidecar", ErrValidation, j.DeliveryKind)
	}
	if !isSafeID(j.ModuleID) {
		return fmt.Errorf("%w: moduleId %q must be a safe ID", ErrValidation, j.ModuleID)
	}
	if !j.State.valid() {
		return fmt.Errorf("%w: state %q not in pending/running/succeeded/failed/compensated", ErrValidation, j.State)
	}
	if !j.DataPolicy.valid() {
		return fmt.Errorf("%w: dataPolicy %q not in retain/purge-cache/purge-data", ErrValidation, j.DataPolicy)
	}
	if strings.TrimSpace(j.Phase) == "" {
		return fmt.Errorf("%w: phase is empty", ErrValidation)
	}
	for _, t := range []struct{ name, value string }{{"createdAt", j.CreatedAt}, {"updatedAt", j.UpdatedAt}} {
		if _, err := time.Parse(time.RFC3339Nano, t.value); err != nil {
			return fmt.Errorf("%w: %s %q is not RFC3339: %v", ErrValidation, t.name, t.value, err)
		}
	}
	for i, st := range j.Steps {
		if strings.TrimSpace(st.Name) == "" {
			return fmt.Errorf("%w: steps[%d].name is empty", ErrValidation, i)
		}
		if !st.State.valid() {
			return fmt.Errorf("%w: steps[%d].state %q not in pending/running/succeeded/failed/compensated", ErrValidation, i, st.State)
		}
	}
	if j.Error != nil && strings.TrimSpace(j.Error.Code) == "" {
		return fmt.Errorf("%w: error.code is empty once error is set", ErrValidation)
	}
	return nil
}

// strictJournalKeys 配合 validate 使用：把落盘文件反序列化进 shadow 结构以
// 检出未知键——§8.3 的十五键即完整契约面，任何旁路键（可能夹带敏感内容）
// 都视为损坏。内存构造的 Journal 经 marshalJournal 序列化后天然只含这些键。
func (j *Journal) unmarshalStrict(data []byte) error {
	known := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	for _, key := range journalFieldNames() {
		delete(known, key)
	}
	if len(known) > 0 {
		leftover := make([]string, 0, len(known))
		for key := range known {
			leftover = append(leftover, key)
		}
		slices.Sort(leftover)
		return fmt.Errorf("%w: unknown fields %v", ErrValidation, leftover)
	}
	return json.Unmarshal(data, j)
}

// journalFieldNames 返回 §8.3 冻结的十五个 JSON 键，供校验与测试共用。
func journalFieldNames() []string {
	return []string{
		"schema", "transactionId", "operation", "deliveryKind", "moduleId",
		"fromVersion", "toVersion", "phase", "state", "createdAt", "updatedAt",
		"activeBefore", "dataPolicy", "steps", "error",
	}
}

// marshalJournal 序列化为 2 空格缩进 JSON（与 internal/jsonstore 落盘口径一致，
// 便于人工审查崩溃现场）。
func marshalJournal(j *Journal) ([]byte, error) {
	return json.MarshalIndent(j, "", "  ")
}

// isSafeID 校验可作为文件名的标识（transactionId 直接映射为
// <transactionId>.json，必须杜绝路径分隔、前导点与越界字符）。
func isSafeID(s string) bool {
	if len(s) < 2 || len(s) > 64 {
		return false
	}
	if !isIDAlnum(rune(s[0])) || !isIDAlnum(rune(s[len(s)-1])) {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return !(isIDAlnum(r) || r == '.' || r == '_' || r == '-')
	}) < 0
}

func isIDAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// nowRFC3339 以 UTC 生成 RFC3339Nano 时间戳（落盘时间统一 UTC，避免跨机同步
// 时区漂移影响恢复顺序判定）。
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// ptrString 便捷构造 *string（fromVersion/toVersion/activeBefore 可空字段）。
func ptrString(s string) *string { return &s }
