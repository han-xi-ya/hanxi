package operation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"hanxi/internal/extapi"
)

// ---------------------------------------------------------------------------
// 哨兵错误
// ---------------------------------------------------------------------------

// ErrTxnActive 表示同一 moduleId 已存在未收口（pending/running）的写事务
// （§8.2 "每个 module ID 同时只允许一个写事务"）。Begin 以它拒绝第二笔事务；
// 调用方据此把冲突呈现为"该模块有操作进行中"，而不是排队或覆盖。
var ErrTxnActive = errors.New("operation: module has an active transaction")

// ErrTxnClosed 表示事务已收口（succeeded/failed/compensated），拒绝再迁移。
// 收口后重试必须走新 transactionId 的新事务（journal 是审计账本，不是可重开工单）。
var ErrTxnClosed = errors.New("operation: transaction already finished")

// ---------------------------------------------------------------------------
// Store：journal 落盘账本
// ---------------------------------------------------------------------------

// 目录名约定（§8.7 步骤 7、§8.8 步骤 3）。本包只盘点这些前缀，创建/清理归
// packages/go/artifact——交接面就是这三个字符串与 transactionId。
const (
	// DirInstallingPrefix 安装/更新中的暂落目录：.installing-<transactionId>
	DirInstallingPrefix = ".installing-"
	// DirRemovingPrefix rename-first 卸载的待删目录：.removing-<transactionId>
	DirRemovingPrefix = ".removing-"
	// DirStagingPrefix 解包/下载 staging 目录：.tmp-<transactionId>
	DirStagingPrefix = ".tmp-"
)

// Store 是 journal 的落盘账本：目录 <dir> 下每笔事务一个文件
// <transactionId>.json（§5.2 目标布局中的 journals/<transaction-id>.json）。
//
// 落盘纪律（§8.2，全部由本类型保证）：
//   - 先落盘并 fsync 再执行副作用：Begin/Advance/Complete 成功返回时内容已
//     Sync 到磁盘；宿主引擎必须在其之后才做下载、解包、rename 等副作用；
//   - 更新走 tmp+rename：写入中途崩溃只遗留 <file>.tmp.<pid> 孤儿文件，
//     目标 journal 要么旧版完好要么新版完好；崩溃遗留的 tmp 孤儿不伪装成
//     事务（扫描只认 *.json 精确后缀），由宿主清理；
//   - 每步状态迁移即持久化：引擎每次 step/phase 迁移都调用一次 Advance；
//   - 同 moduleId 单写事务：Begin 持锁检查未收口事务集合，冲突返回 ErrTxnActive。
//
// dir 语义：调用方根传入。宿主既可以让 dir 同时容纳 journal 与
// .installing-*/.removing-*/.tmp-*（AbandonedDirs 就地盘点），也可以按 §5.2
// 布局给 journals 与事务资产根各开一个 Store——后者的 journal 扫描自然为空，
// 只当 AbandonedDirs 探针使用。
//
// 并发模型：单进程内以 mu 串行化全部读写；跨进程一致性依赖"恢复完成后才
// 开放业务"（§8.8）保证启动期独占。损坏文件（无法解析/校验不过/含未知键）
// 不伪装成空账：Pending 跳过它们并如实报告，Recover 负责隔离处置。
type Store struct {
	dir string
	mu  sync.Mutex
}

// OpenStore 确保目录存在并返回账本句柄（不落任何初始文件）。
func OpenStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("%w: store dir is empty", ErrValidation)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("operation: create journal dir %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Dir 返回账本根目录（宿主拼 .installing-* 等约定目录名时用）。
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(txnID string) string {
	return filepath.Join(s.dir, txnID+".json")
}

// Begin 落盘一笔新事务：补默认值 → 全量校验 → 同模块单写检查 →
// O_EXCL 独占创建 + fsync。默认值：schema=Schema、state=pending、
// dataPolicy=retain（§9.2 默认档）、createdAt/updatedAt=当前 UTC、steps=[]；
// transactionId/operation/deliveryKind/moduleId/phase 必须显式给出；
// 起始 state 只允许 pending/running。
//
// 文件已存在（同 transactionId 重放）返回包装 fs.ErrExist 的错误：同一事务
// 只能 Begin 一次，重放方应改用 Get/Advance。损坏文件不阻塞 Begin——但其
// 可能掩盖同模块的隐形活跃事务，因此 §8.8"恢复先于业务"是单写纪律成立的
// 前提（宿主必须先 Recover 或至少隔离损坏账，再放行写路径）。
func (s *Store) Begin(j Journal) error {
	if j.Schema == 0 {
		j.Schema = Schema
	}
	if j.State == "" {
		j.State = TxnPending
	}
	if j.DataPolicy == "" {
		j.DataPolicy = DataRetain
	}
	if j.Steps == nil {
		j.Steps = []Step{}
	}
	if j.CreatedAt == "" {
		j.CreatedAt = nowRFC3339()
	}
	if j.UpdatedAt == "" {
		j.UpdatedAt = j.CreatedAt
	}
	if err := j.validate(); err != nil {
		return err
	}
	if j.State.terminal() || j.State == TxnCompensated {
		return fmt.Errorf("%w: begin state %q not allowed (pending/running only)", ErrValidation, j.State)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	live, _, err := s.pendingLocked()
	if err != nil {
		return err
	}
	for _, p := range live {
		// 单写判定只看 pending/running：compensated 事务的现场已交回恢复流，
		// 不阻塞该模块的新事务（其收口由 Complete 完成）。
		if p.ModuleID == j.ModuleID && (p.State == TxnPending || p.State == TxnRunning) {
			return fmt.Errorf("%w: module %q is occupied by unfinished transaction %q (%s/%s)",
				ErrTxnActive, j.ModuleID, p.TransactionID, p.Operation, p.State)
		}
	}

	data, err := marshalJournal(&j)
	if err != nil {
		return fmt.Errorf("operation: marshal journal %q: %w", j.TransactionID, err)
	}
	// 先落盘再副作用（§8.2）：O_EXCL 独占创建杜绝覆盖既有账；Sync 成功
	// 返回前，本事务对任何后续崩溃都可见。
	path := s.path(j.TransactionID)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("operation: create journal %q: %w", path, err)
	}
	if err := writeAndSync(f, data); err != nil {
		f.Close()
		os.Remove(path)
		return fmt.Errorf("operation: write journal %q: %w", path, err)
	}
	return nil
}

// Advance 记录阶段与步骤进度："读-校验-派生-fsync-原子替换"，每步状态迁移
// 即持久化（§8.2）。phase 为空串则保持原阶段不变；steps 是全量快照（非追加），
// 由 Advance 从步骤态自动派生事务级 state：
//
//	steps 为空                      → pending（尚未展开）
//	全部 step succeeded             → succeeded
//	任一 step failed                → failed
//	其余（pending/running/compensated）→ running
//
// 调用方不直接设事务级 state，也无法经本方法写 compensated 或 Error——
// compensated 只由 Complete 落账，失败错误信息随 Complete(finalState=failed)
// 的 opErr 写入。事务已收口（succeeded/failed）后返回 ErrTxnClosed；
// 未收口的 compensated 事务允许继续 Advance（恢复流接管后推进补偿步骤）。
// transactionId/moduleId 等身份字段在迁移中保持不变。
func (s *Store) Advance(txnID string, phase string, steps []Step) error {
	if !isSafeID(txnID) {
		return fmt.Errorf("%w: transactionId %q is not a safe ID", ErrValidation, txnID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	j, err := s.readLocked(txnID)
	if err != nil {
		return err
	}
	if j.State.terminal() {
		return fmt.Errorf("%w: transaction %q is %s", ErrTxnClosed, txnID, j.State)
	}
	if phase != "" {
		j.Phase = phase
	}
	j.Steps = append([]Step{}, steps...) // 全量快照：nil 也归一为 []，落盘不留 null
	j.State = deriveState(j.Steps)
	j.UpdatedAt = nowRFC3339()
	if err := j.validate(); err != nil {
		return fmt.Errorf("operation: journal %q after advance: %w", txnID, err)
	}
	return s.writeAtomic(j)
}

// deriveState 按 steps 快照派生事务级状态（映射表见 Advance 注释）。
func deriveState(steps []Step) TxnState {
	if len(steps) == 0 {
		return TxnPending
	}
	allSucceeded := true
	for _, st := range steps {
		switch st.State {
		case TxnFailed:
			return TxnFailed
		case TxnSucceeded:
		default:
			allSucceeded = false
		}
	}
	if allSucceeded {
		return TxnSucceeded
	}
	return TxnRunning
}

// Complete 收口事务。finalState ∈ {succeeded, failed, compensated}：
//   - succeeded/failed 为终态，收口后账本对本 Store 的全部变更方法只读；
//   - compensated 是允许的直接收口态（恢复流就地完成补偿后的落账），但它
//     不算终态——仍出现在 Pending 里、仍允许 Advance 继续推进（如卸载目录
//     待重启删除的现场），最终由后续 Complete 落到 succeeded/failed；
//   - failed 必须携带 opErr（code 非空；message 由调用方保证不含 Secret/
//     敏感命令行，§8.2），非 failed 收口拒绝传 opErr；
//   - 同一终态重复 Complete 幂等返回 nil（崩溃重放安全）；已收口为其他
//     终态返回 ErrTxnClosed（拒绝"翻案"，审计账本单收口）。
func (s *Store) Complete(txnID string, finalState string, opErr *extapi.OperationError) error {
	want := TxnState(finalState)
	if want != TxnSucceeded && want != TxnFailed && want != TxnCompensated {
		return fmt.Errorf("%w: complete state %q must be succeeded/failed/compensated", ErrValidation, finalState)
	}
	if want == TxnFailed && (opErr == nil || strings.TrimSpace(opErr.Code) == "") {
		return fmt.Errorf("%w: failed completion requires an opErr with a code", ErrValidation)
	}
	if want != TxnFailed && opErr != nil {
		return fmt.Errorf("%w: opErr is only allowed for failed completion", ErrValidation)
	}
	if !isSafeID(txnID) {
		return fmt.Errorf("%w: transactionId %q is not a safe ID", ErrValidation, txnID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	j, err := s.readLocked(txnID)
	if err != nil {
		return err
	}
	if j.State == want {
		if want != TxnFailed || j.Error != nil {
			return nil // 同一收口态重放：幂等
		}
		// failed 但缺错误信息（Advance 派生出的 failed 现场）：允许补写一次 opErr。
		clone := *opErr
		j.Error = &clone
		j.UpdatedAt = nowRFC3339()
		if err := j.validate(); err != nil {
			return fmt.Errorf("operation: journal %q after error backfill: %w", txnID, err)
		}
		return s.writeAtomic(j)
	}
	if j.State.terminal() {
		return fmt.Errorf("%w: transaction %q is %s, cannot complete as %s", ErrTxnClosed, txnID, j.State, want)
	}
	j.State = want
	if want == TxnFailed {
		clone := *opErr
		j.Error = &clone
	}
	j.UpdatedAt = nowRFC3339()
	if err := j.validate(); err != nil {
		return fmt.Errorf("operation: journal %q after completion: %w", txnID, err)
	}
	return s.writeAtomic(j)
}

// Get 读取一笔事务的当前落盘状态（含已收口事务）。
func (s *Store) Get(txnID string) (Journal, error) {
	if !isSafeID(txnID) {
		return Journal{}, fmt.Errorf("%w: transactionId %q is not a safe ID", ErrValidation, txnID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.readLocked(txnID)
	if err != nil {
		return Journal{}, err
	}
	return *j, nil
}

// Pending 返回全部未收口事务——state ∈ {pending, running, compensated}：
// compensated 不算终态（恢复流可能仍需其现场继续 Advance 或最终 Complete 落账），
// 只有 succeeded/failed 才算收口。按 (createdAt, transactionId)
// 稳定排序（§8.8 "扫描未完成 journal"：恢复顺序可重现）。
// 损坏文件被跳过——它们既不是"未完成"也不是"已完成"，由 Recover 隔离处置；
// 宿主在业务放行前应确认 Recover 报告无 Quarantined 遗留。
func (s *Store) Pending() ([]Journal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, _, err := s.pendingLocked()
	return live, err
}

// AbandonedDirs 盘点账本根下符合目录名约定、但没有任何未收口事务背书的孤儿
// 目录（§8.8 步骤 3）：
//   - installing：.installing-*（解包半途遗留）
//   - removing：  .removing-*（rename-first 卸载删一半遗留）
//   - staging：   .tmp-*（下载/解包工作区遗留）
//
// "孤儿"判据：目录名后缀 <transactionId> 若无对应 journal（或 journal 已收口），
// 即计入返回值；有未收口事务背书的目录是进行中的合法现场，不属于盘点结果。
// 本方法纯只读：删除/回滚决策归宿主恢复流程（可能对接 artifact 或隔离区）。
func (s *Store) AbandonedDirs() (installing, removing, staging []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, _, err := s.pendingLocked()
	if err != nil {
		return nil, nil, nil, err
	}
	active := map[string]bool{} // transactionId -> 有未收口账本
	for _, j := range live {
		active[j.TransactionID] = true
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("operation: scan %q: %w", s.dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isOrphanDir(e.Name(), active) {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		switch {
		case strings.HasPrefix(e.Name(), DirInstallingPrefix):
			installing = append(installing, path)
		case strings.HasPrefix(e.Name(), DirRemovingPrefix):
			removing = append(removing, path)
		default: // DirStagingPrefix（isOrphanDir 已保证三类之一）
			staging = append(staging, path)
		}
	}
	slices.Sort(installing)
	slices.Sort(removing)
	slices.Sort(staging)
	return installing, removing, staging, nil
}

// isOrphanDir 判定目录名（<前缀><transactionId>）是否无未收口事务背书。
// 前缀剥离后不是安全 ID（手工造的名）→ 同样按孤儿上报，宁可多报不可漏报。
func isOrphanDir(name string, active map[string]bool) bool {
	for _, p := range []string{DirInstallingPrefix, DirRemovingPrefix, DirStagingPrefix} {
		if strings.HasPrefix(name, p) {
			txn := strings.TrimPrefix(name, p)
			return !isSafeID(txn) || !active[txn]
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 内部读写
// ---------------------------------------------------------------------------

// readLocked 读取并严格校验一笔事务（须持 s.mu）。文件缺失原样透传
// fs.ErrNotExist；解析/校验失败或含未知键返回 ErrCorrupt（不伪装成空账）。
func (s *Store) readLocked(txnID string) (*Journal, error) {
	path := s.path(txnID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("operation: read journal %q: %w", path, err)
	}
	var j Journal
	if err := j.unmarshalStrict(data); err != nil {
		return nil, fmt.Errorf("%w %q: %v", ErrCorrupt, path, err)
	}
	if err := j.validate(); err != nil {
		return nil, fmt.Errorf("%w %q: %v", ErrCorrupt, path, err)
	}
	if j.TransactionID != txnID {
		return nil, fmt.Errorf("%w %q: transactionId mismatch (file says %q)", ErrCorrupt, path, j.TransactionID)
	}
	return &j, nil
}

// writeAtomic 以 tmp+fsync+rename 原子替换事务文件（须持 s.mu）。
func (s *Store) writeAtomic(j *Journal) error {
	data, err := marshalJournal(j)
	if err != nil {
		return fmt.Errorf("operation: marshal journal %q: %w", j.TransactionID, err)
	}
	final := s.path(j.TransactionID)
	tmp := fmt.Sprintf("%s.tmp.%d", final, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("operation: create tmp %q: %w", tmp, err)
	}
	if err := writeAndSync(f, data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("operation: write tmp %q: %w", tmp, err)
	}
	// Windows 上 os.Rename 走 MoveFileEx(REPLACE_EXISTING)，同卷原子替换；
	// POSIX 上 rename 本身原子。崩溃只会遗留 tmp 孤儿（扫描不认 *.json 后缀）。
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("operation: rename tmp over %q: %w", final, err)
	}
	return nil
}

// writeAndSync 写入并 Sync：Sync/Close 失败视同写入失败（"先落盘再副作用"
// 的承诺不允许半途）。注：Windows 无目录项 fsync，文件名可见性依赖
// NTFS 同卷 rename 的事务性（tmp 孤儿模式已兜底）。
func writeAndSync(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// pendingLocked 扫描全目录（须持 s.mu），返回按 (createdAt, transactionId)
// 稳定排序的未收口事务与损坏文件路径清单。
func (s *Store) pendingLocked() ([]Journal, []string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil // 目录被外部删除：视为空账，不报错
		}
		return nil, nil, fmt.Errorf("operation: scan %q: %w", s.dir, err)
	}
	var live []Journal
	var corrupts []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue // 目录是 artifact 侧现场；*.tmp.<pid> 是崩溃孤儿 tmp
		}
		stem := strings.TrimSuffix(name, ".json")
		path := filepath.Join(s.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			corrupts = append(corrupts, path)
			continue
		}
		var j Journal
		if err := j.unmarshalStrict(data); err != nil {
			corrupts = append(corrupts, path)
			continue
		}
		if err := j.validate(); err != nil || !isSafeID(stem) || j.TransactionID != stem {
			corrupts = append(corrupts, path)
			continue
		}
		if !j.State.terminal() {
			live = append(live, j)
		}
	}
	slices.SortStableFunc(live, func(a, b Journal) int {
		ta, _ := time.Parse(time.RFC3339Nano, a.CreatedAt)
		tb, _ := time.Parse(time.RFC3339Nano, b.CreatedAt)
		if !ta.Equal(tb) {
			return ta.Compare(tb)
		}
		return strings.Compare(a.TransactionID, b.TransactionID)
	})
	return live, corrupts, nil
}
