// receipts.go 逻辑安装凭据（receipt）的目录文件存储（Wave 1 后端底座，
// 存储契约冻结于 internal/extapi/receipt.go），及其"模块名单账本"
// （modules-ledger.json，P0 批 1 项二改造，设计见 docs/plans/PLAN_P0_BATCH1_FIX.md）。
//
// 布局：<dir>/<moduleId>.json 每模块一份，内容即 extapi.Receipt JSON；
// 账本独立于 receipts 命名空间之外（state/modules-ledger.json），根治
// "known-modules.json 混在凭据目录里被扫描误报、未来同名模块路径冲突"
// 以及"账本蹭契约 schema 被无关升版重置"两项成组缺陷。
// 读取纪律：磁盘缺失 = 未安装；损坏文件按未安装处理但 slog.Warn 留痕——
// Registry 投影不得被个别坏文件阻断。写入纪律：复用 writeAtomicFile
// （tmp+rename，对齐 Store.saveLocked），写失败旧凭据原样保留，错误中文可诊断。
// 安全纪律：moduleId 直接参与文件名拼接，必须先过消毒闸门（仅 [a-z0-9-]），
// 拒绝路径分隔、上级跳出与空名，防路径注入；账本相关保留名单独拒绝。
//
// 卸载跨重启不变量（fail-closed）：MarkAbsent 先把 tombstone 落账本
// （主文件 + .bak 双写）成功，才动 receipt；账本写失败即卸载报错、凭据
// 原样可重试——绝不出现"表面卸载、重启复活"。
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/extapi"
)

// 编译期断言：ReceiptStore 必须完整实现 Wave 0 冻结的存储契约。
var _ extapi.ReceiptStorage = (*ReceiptStore)(nil)

// receiptFileSuffix 凭据文件名后缀。
const receiptFileSuffix = ".json"

// ReceiptStore 目录文件型安装凭据存储，实现 extapi.ReceiptStorage。
//
// 无内存缓存：凭据与账本都是低频小文件，读取直达磁盘即可，磁盘保持单一
// 事实源（安装态的批量投影由 Registry 侧在内存汇总，不归本存储管）。
// mu 为 RWMutex：写入侧（MarkInstalled/MarkAbsent/EnsureSeen）持写锁护住
// "读-判-写" 序列——幂等判定、账本落款与删除若被并发穿插，会交错出形态
// 漂移或删后复活的孤儿文件；读取侧（IsInstalled/Installed）持读锁与写侧
// 互斥，保证投影内不混用同一模块的改前/改后态。
type ReceiptStore struct {
	dir        string // receipts 目录（仅存凭据文件）
	ledgerFile string // 模块名单账本路径（state/modules-ledger.json，独立命名空间）
	mu         sync.RWMutex
}

// NewReceiptStore 创建凭据存储：receiptsDir 存凭据，ledgerFile 指向名单
// 账本（装配根经 paths.ModulesLedgerFile() 提供；同目录的旧版
// known-modules.json 会在首轮 EnsureSeen 一次性迁移）。
// 目录预建归数据根布局 ensureDirs（见 paths.go）；构造函数不做任何 IO，
// 测试可直接注入 t.TempDir()。
func NewReceiptStore(receiptsDir, ledgerFile string) *ReceiptStore {
	return &ReceiptStore{dir: receiptsDir, ledgerFile: ledgerFile}
}

// reservedModuleIDs 与账本/迁移文件共享词法的保留名单：合法化即封死
// "未来出现同名模块与旧版账本文件路径冲突"的通道（批 1 项二 #5）。
var reservedModuleIDs = map[string]bool{"known-modules": true}

// sanitizeModuleID moduleId 消毒闸门：仅允许小写字母、数字与连字符且非空，
// 且不得命中保留名。该字符集天然排除路径分隔符、"."、".."、盘符与空字节，
// 通过校验即可安全拼名。
func sanitizeModuleID(moduleID string) error {
	if moduleID == "" {
		return errors.New("receipt: moduleId 不能为空")
	}
	for _, r := range moduleID {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("receipt: moduleId %q 含非法字符（仅允许小写字母、数字与连字符）", moduleID)
	}
	if reservedModuleIDs[moduleID] {
		return fmt.Errorf("receipt: moduleId %q 为账本保留名，不可用作模块 ID", moduleID)
	}
	return nil
}

// receiptPath 拼凭据文件绝对路径。调用前必须已过 sanitizeModuleID。
func (s *ReceiptStore) receiptPath(moduleID string) string {
	return filepath.Join(s.dir, moduleID+receiptFileSuffix)
}

// readReceipt 读取并解析单个凭据文件，返回 (凭据, 存在, 错误) 三态：
// 文件不存在 → (零值, false, nil)，即未安装；读取或解析失败、内容
// moduleId 与文件名不符（手工拷贝漂移）→ 错误。调用方按
// "读 fail soft、写 fail loud" 纪律决定吞告警还是上抛。
// 调用方必须已持有 mu（读锁或写锁）。
func (s *ReceiptStore) readReceipt(moduleID string) (extapi.Receipt, bool, error) {
	raw, err := os.ReadFile(s.receiptPath(moduleID))
	if errors.Is(err, fs.ErrNotExist) {
		return extapi.Receipt{}, false, nil
	}
	if err != nil {
		return extapi.Receipt{}, false, fmt.Errorf("读取凭据文件失败: %w", err)
	}
	var rec extapi.Receipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		return extapi.Receipt{}, false, fmt.Errorf("凭据 JSON 损坏: %w", err)
	}
	if rec.ModuleID != moduleID {
		return extapi.Receipt{}, false, fmt.Errorf("凭据内容 moduleId %q 与文件名不符", rec.ModuleID)
	}
	return rec, true, nil
}

// IsInstalled 查询模块是否持有有效 receipt。非法 ID 静默返回 false
// （查询侧对脏输入拒绝即可，报错是写入侧的职责）；缺失与损坏均按
// 未安装处理，后者 slog.Warn 留痕。
func (s *ReceiptStore) IsInstalled(moduleID string) bool {
	if err := sanitizeModuleID(moduleID); err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, found, err := s.readReceipt(moduleID)
	if err != nil {
		slog.Warn("安装凭据读取异常，按未安装处理", "moduleId", moduleID, "path", s.receiptPath(moduleID), "err", err)
		return false
	}
	return found
}

// MarkInstalled 登记/确认逻辑安装凭据（幂等）：
//   - 同 ID 同 kind 重复登记：保留首次 InstalledAt 直接返回 nil——
//     重写会无谓触碰磁盘，且时刻漂移让"首次安装"失真；
//   - 同 ID 不同 kind：拒绝。kind 由装配根目录表钉死，落盘后漂移说明
//     代码与目录表对不上，必须 fail loud 立案查表，绝不静默改写掩盖；
//   - 已存在但损坏：按未安装处理，重写一份干净凭据（留痕 Warn）。
//
// 落盘复用 writeAtomicFile（tmp+rename），任一步失败旧文件原样保留。
func (s *ReceiptStore) MarkInstalled(moduleID string, kind extapi.ReceiptKind) error {
	if err := sanitizeModuleID(moduleID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.markInstalledLocked(moduleID, kind); err != nil {
		return err
	}
	// 显式安装成功即改判：seen 入账、tombstone 注销（用户重新安装 = 卸载记忆翻篇）。
	// 账本写失败不撤销已落盘凭据（安装本身成功，下轮 EnsureSeen 会重试改判），
	// 但错误必须上抛——"卸载记忆是否还在"是用户可见语义，不允许静默漂移。
	return s.markSeenLocked(moduleID)
}

// markInstalledLocked 落盘凭据的主体（调用方必须已持有 s.mu 写锁）。
func (s *ReceiptStore) markInstalledLocked(moduleID string, kind extapi.ReceiptKind) error {
	rec, found, err := s.readReceipt(moduleID)
	switch {
	case err != nil:
		slog.Warn("安装凭据异常，按新装重写", "moduleId", moduleID, "err", err)
	case found && rec.Kind == kind:
		return nil
	case found && rec.Kind != kind:
		return fmt.Errorf("receipt: 模块 %s 交付形态漂移（凭据 %q → 目录表 %q），拒绝改写", moduleID, rec.Kind, kind)
	}

	raw, err := json.MarshalIndent(extapi.Receipt{
		Schema:      extapi.ModuleContractSchema,
		ModuleID:    moduleID,
		Kind:        kind,
		InstalledAt: time.Now(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("receipt: 序列化模块 %s 凭据失败: %w", moduleID, err)
	}
	if err := writeAtomicFile(s.receiptPath(moduleID), raw); err != nil {
		return fmt.Errorf("receipt: 落盘模块 %s 凭据失败: %w", moduleID, err)
	}
	return nil
}

// MarkAbsent 移除凭据（逻辑卸载），fail-closed 顺序（批 1 项二 #3）：
//  1. tombstone 先落账本（主文件+.bak 双写成功为止）——卸载记忆优先于卸载动作；
//  2. 账本写失败 → 立即报错返回，凭据原样保留（用户可重试，绝无"半卸载"）；
//  3. 凭据删除：文件不存在视为已卸载（幂等）。若 tombstone 已落而删除失败，
//     下轮投影仍显示已安装但 EnsureSeen 永不补建，重试卸载即收敛——两种失败
//     形态都不复活、都不谎报。
//
// 模块数据目录不在凭据职责内，此处绝不删除。
func (s *ReceiptStore) MarkAbsent(moduleID string) error {
	if err := sanitizeModuleID(moduleID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.markUninstalledLocked(moduleID); err != nil {
		return err
	}
	if err := os.Remove(s.receiptPath(moduleID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("receipt: 删除模块 %s 凭据失败: %w", moduleID, err)
	}
	return nil
}

// Installed 扫描凭据目录，返回全部有效凭据集合（Registry 投影用，
// 键为 moduleId、值恒 true）。目录缺失按空集静默返回（全新安装常态）；
// 目录不可读、非 *.json 文件名、非法 ID 名、解析失败的文件一律跳过并
// slog.Warn——投影宁可保守漏报，不可被单个坏文件 panic 或阻断。
func (s *ReceiptStore) Installed() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.installedLocked()
}

// installedLocked 凭据目录扫描（调用方须已持读锁或写锁；账本三态重建复用）。
func (s *ReceiptStore) installedLocked() map[string]bool {
	out := make(map[string]bool)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("扫描安装凭据目录失败，投影按空集处理", "dir", s.dir, "err", err)
		}
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, receiptFileSuffix) {
			continue
		}
		if name == legacyLedgerFile {
			continue // 旧版账本迁移失败残留：静默跳过（迁移成功即改名离场）
		}
		moduleID := strings.TrimSuffix(name, receiptFileSuffix)
		if err := sanitizeModuleID(moduleID); err != nil {
			slog.Warn("安装凭据文件名非法，跳过", "dir", s.dir, "file", name, "err", err)
			continue
		}
		if _, found, err := s.readReceipt(moduleID); err != nil || !found {
			slog.Warn("安装凭据解析失败，按未安装跳过", "moduleId", moduleID, "err", err)
			continue
		}
		out[moduleID] = true
	}
	return out
}

// EnsureInstalled 迁移辅助：为给定模块集合幂等补建 receipt（老用户升级时，
// 装配根把"历史上一直可用"的存量模块补登进凭据体系）。逐个走与
// MarkInstalled 完全相同的幂等闸门——已存在同 kind 不动、漂移拒绝、
// 写失败仅波及该模块且旧凭据原样保留；错误聚合返回（批量迁移中一个
// 坏 ID 不应中断其余补建，但绝不静默吞错）。
func (s *ReceiptStore) EnsureInstalled(moduleIDs []string, kind extapi.ReceiptKind) error {
	var errs []error
	for _, id := range moduleIDs {
		if err := s.MarkInstalled(id, kind); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ---------- 模块名单账本（modules-ledger.json，独立于 receipts 命名空间） ----------

// legacyLedgerFile 旧版账本文件名（曾在 receipts 目录内蹭 *.json 命名空间，
// 批 1 项二 #5）。首轮 EnsureSeen 一次性迁移后改名 .migrated 离场；
// Installed() 对迁移失败残留另有跳过护栏。
const legacyLedgerFile = "known-modules.json"

// ledgerSchemaVersion 账本自有版本线（批 1 项二 #4：不再蹭 ModuleContractSchema，
// 前端/状态契约升版与卸载记忆再无瓜葛）。
const ledgerSchemaVersion = 1

// moduleLedger 账本落盘形状：seen（登记过，EnsureSeen 永不补建）与
// uninstalled（tombstone：永不补建，仅显式安装可注销）。两表定序落盘，
// seen∩uninstalled 恒空（saveLedgerLocked 收口）。
type moduleLedger struct {
	Schema      int      `json:"schema"`
	Seen        []string `json:"seen"`
	Uninstalled []string `json:"uninstalled"`
}

// legacyKnownLedger 旧版账本形状（仅作迁移输入）。
type legacyKnownLedger struct {
	Schema    int      `json:"schema"`
	ModuleIDs []string `json:"moduleIds"`
}

// ledgerState 账本装载结果（三态判定产物，见 loadLedgerLocked）。
type ledgerState struct {
	ledger   moduleLedger
	readOnly bool // 更高未知 schema：全场禁写
	rebuilt  bool // 本轮按磁盘凭据事实重建（注册缺席模块将判"已卸载"）
	migrated bool // 本轮完成了旧账本迁移（需回写）
}

// loadLedgerLocked 账本装载唯一入口（调用方须持写锁）。三态语义（批 1 项二 #1/#2）：
//   - 在场且版本合规 → 按记录执行；
//   - 解析失败 → 先试 .bak 救回（视同损坏修复，dirty 由后续写路径回正主文件）；
//     仍败降级按"文件缺失"处理并 slog.Error 留痕；
//   - 文件缺失 + receipts 目录非空 → "升级首次引入/账本丢失"：按磁盘凭据事实
//     重建（在场=seen；注册但缺席=uninstalled）——"缺席=已卸载"的保守裁决
//     根除丢失/损坏后的静默复活（机主已确认语义）；
//   - 文件缺失 + receipts 目录空 → 全新安装：空账本起步，EnsureSeen 全量认全；
//   - schema 高于当前版本 → 只读模式：不改写、不补建，卸载/显式安装改判
//     显式报错（不摧毁未来版本字段，fail-closed 比假装成功诚实）。
//
// 旧版 known-modules.json 在场且新账本缺席/损坏时优先走迁移路径：
// 旧名单 ∩ 凭据事实 归位 seen/uninstalled，旧文件随即改名 .migrated 离场。
func (s *ReceiptStore) loadLedgerLocked() (ledgerState, error) {
	var errs []error
	raw, err := os.ReadFile(s.ledgerFile)
	switch {
	case err == nil:
		var led moduleLedger
		if uerr := json.Unmarshal(raw, &led); uerr != nil {
			slog.Error("模块账本解析失败，尝试备份救回", "path", s.ledgerFile, "err", uerr)
			if bak, berr := os.ReadFile(s.ledgerFile + ".bak"); berr == nil {
				var led2 moduleLedger
				if json.Unmarshal(bak, &led2) == nil {
					st := ledgerState{ledger: led2}
					if led2.Schema > ledgerSchemaVersion {
						// 未来版本备份：只读接管，不越版本重写主文件。
						st.readOnly = true
						slog.Warn("模块账本主文件损坏且备份为更高版本 schema，进入只读模式", "path", s.ledgerFile)
						return st, nil
					}
					slog.Warn("模块账本已由备份救回", "path", s.ledgerFile)
					if serr := s.saveLedgerLocked(led2); serr != nil {
						slog.Warn("主文件修复写回失败，后续写操作将再次尝试", "err", serr)
					}
					return st, nil
				}
			}
			// 主备俱坏：降级"文件缺失"路径。
			return s.rebuildLedgerLocked()
		}
		if led.Schema > ledgerSchemaVersion {
			slog.Warn("模块账本为更高版本 schema，本进程只读（不补建、不改写）",
				"schema", led.Schema, "supported", ledgerSchemaVersion)
			return ledgerState{ledger: led, readOnly: true}, nil
		}
		if led.Schema < ledgerSchemaVersion {
			// 历史版本线升级：当前仅 v1 一代，补版本号（后续代次在此插迁移）。
			led.Schema = ledgerSchemaVersion
		}
		return ledgerState{ledger: led}, nil
	case !errors.Is(err, fs.ErrNotExist):
		errs = append(errs, fmt.Errorf("读取模块账本异常（按文件缺失处理）: %w", err))
	}
	st, rerr := s.rebuildLedgerLocked()
	return st, errors.Join(append(errs, rerr)...)
}

// rebuildLedgerLocked "文件缺失"分支：优先旧账本迁移，否则按凭据事实/全新安装重建。
func (s *ReceiptStore) rebuildLedgerLocked() (ledgerState, error) {
	installed := s.installedLocked()
	var errs []error
	st := ledgerState{rebuilt: true}

	legacyPath := filepath.Join(s.dir, legacyLedgerFile)
	if raw, err := os.ReadFile(legacyPath); err == nil {
		var old legacyKnownLedger
		if json.Unmarshal(raw, &old) == nil {
			for _, id := range old.ModuleIDs {
				if sanitizeModuleID(id) != nil {
					continue // 保留名/脏 ID 不入账（脏数据不传染）
				}
				if installed[id] {
					st.ledger.Seen = append(st.ledger.Seen, id)
					delete(installed, id)
				} else {
					// 旧机制的卸载记忆 = "known 且无凭据"：归位 tombstone。
					st.ledger.Uninstalled = append(st.ledger.Uninstalled, id)
				}
			}
			st.migrated = true
			if rerr := os.Rename(legacyPath, legacyPath+".migrated"); rerr != nil {
				errs = append(errs, fmt.Errorf("旧账本迁移后改名失败（Installed 已有跳过护栏）: %w", rerr))
			}
		} else {
			slog.Warn("旧版账本不可解析，忽略并按凭据事实重建", "path", legacyPath)
			_ = os.Rename(legacyPath, legacyPath+".corrupt")
		}
	}

	if len(installed) == 0 && !st.migrated && emptyDirOrMissing(s.dir) {
		// 全新安装（凭据目录空/不存在且无可迁移旧账本）：空账本起步。
		st.rebuilt = false
	} else {
		for id := range installed {
			st.ledger.Seen = append(st.ledger.Seen, id)
		}
	}
	st.ledger.Schema = ledgerSchemaVersion
	return st, errors.Join(errs...)
}

// emptyDirOrMissing 目录不存在或无任何文件 → true（全新安装判定；读取异常按
// 非空保守处理，宁走重建不误判全新）。
func emptyDirOrMissing(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	return len(entries) == 0
}

// saveLedgerLocked 账本落盘：去重、seen∩uninstalled 清界（uninstalled 让位给
// seen——显式安装赢）、双表定序，主文件原子写 + .bak 副本（副本失败仅告警：
// 主文件成功即语义达成，.bak 是灾备冗余不是事务主体）。
func (s *ReceiptStore) saveLedgerLocked(led moduleLedger) error {
	seen := map[string]bool{}
	for _, id := range led.Seen {
		if sanitizeModuleID(id) == nil {
			seen[id] = true
		}
	}
	var gone []string
	for _, id := range led.Uninstalled {
		if sanitizeModuleID(id) != nil || seen[id] {
			continue
		}
		gone = append(gone, id)
	}
	var seenIDs []string
	for id := range seen {
		seenIDs = append(seenIDs, id)
	}
	sort.Strings(seenIDs)
	sort.Strings(gone)
	led.Schema = ledgerSchemaVersion
	led.Seen, led.Uninstalled = seenIDs, gone

	raw, err := json.MarshalIndent(led, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化模块账本失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.ledgerFile), 0o755); err != nil {
		return fmt.Errorf("创建账本目录失败: %w", err)
	}
	if err := writeAtomicFile(s.ledgerFile, raw); err != nil {
		return fmt.Errorf("写入模块账本失败: %w", err)
	}
	if werr := os.WriteFile(s.ledgerFile+".bak", raw, 0o644); werr != nil {
		slog.Warn("模块账本备份写入失败（主文件已成，灾备冗余降级）", "err", werr)
	}
	return nil
}

// mutateLedgerLocked 读-改-写单通道：只读模式显式拒绝（fail-closed）。
func (s *ReceiptStore) mutateLedgerLocked(edit func(*moduleLedger)) error {
	st, err := s.loadLedgerLocked()
	if err != nil {
		return err
	}
	if st.readOnly {
		return errors.New("模块账本为更高版本 schema（可能来自新版 Hanxi），本版本拒绝改写账本相关状态；请升级 Hanxi 后重试")
	}
	edit(&st.ledger)
	return s.saveLedgerLocked(st.ledger)
}

// markSeenLocked 显式安装改判：入 seen、销 tombstone。
func (s *ReceiptStore) markSeenLocked(moduleID string) error {
	return s.mutateLedgerLocked(func(led *moduleLedger) {
		led.Seen = append(led.Seen, moduleID)
		out := led.Uninstalled[:0]
		for _, id := range led.Uninstalled {
			if id != moduleID {
				out = append(out, id)
			}
		}
		led.Uninstalled = out
	})
}

// markUninstalledLocked 卸载第一步（fail-closed 前置）：入 tombstone、出 seen。
func (s *ReceiptStore) markUninstalledLocked(moduleID string) error {
	return s.mutateLedgerLocked(func(led *moduleLedger) {
		led.Uninstalled = append(led.Uninstalled, moduleID)
		out := led.Seen[:0]
		for _, id := range led.Seen {
			if id != moduleID {
				out = append(out, id)
			}
		}
		led.Seen = out
	})
}

// EnsureSeen 以当前注册模块全集驱动一次性迁移与增量安装。新语义（批 1 项二）：
//   - seen ∪ uninstalled 恒跳过（卸载永久生效；tombstone 只有显式安装能注销）；
//   - 正常账本下"新看见"的模块补建 builtin-logical 凭据并入 seen；
//   - 重建态（rebuilt）下注册但磁盘无凭据的模块判"缺席=已卸载"→ 落 tombstone
//     且不补建（保守裁决，机主已确认）；
//   - 只读态（更高 schema）不做任何补建/写入，仅告警返回；
//   - 迁移/重建产物随本轮统一落盘。
//
// 返回本轮新补建凭据的模块 ID 列表；失败聚合上抛但不中断其余补建。
func (s *ReceiptStore) EnsureSeen(registeredIDs []string, kind extapi.ReceiptKind) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadLedgerLocked()
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	if st.readOnly {
		return nil, errors.Join(errs...)
	}

	seen := make(map[string]bool, len(st.ledger.Seen))
	for _, id := range st.ledger.Seen {
		seen[id] = true
	}
	gone := make(map[string]bool, len(st.ledger.Uninstalled))
	for _, id := range st.ledger.Uninstalled {
		gone[id] = true
	}

	var added []string
	touched := st.rebuilt || st.migrated
	for _, id := range registeredIDs {
		if serr := sanitizeModuleID(id); serr != nil {
			errs = append(errs, serr)
			continue
		}
		if seen[id] || gone[id] {
			continue
		}
		if st.rebuilt {
			gone[id] = true // 缺席=已卸载（保守裁决）：落 tombstone，不补建
			touched = true
			continue
		}
		if merr := s.markInstalledLocked(id, kind); merr != nil {
			errs = append(errs, merr)
			continue // 失败的模块不入账，下次启动重试
		}
		seen[id] = true
		added = append(added, id)
		touched = true
	}

	led := st.ledger
	led.Seen = make([]string, 0, len(seen))
	for id := range seen {
		led.Seen = append(led.Seen, id)
	}
	led.Uninstalled = make([]string, 0, len(gone))
	for id := range gone {
		led.Uninstalled = append(led.Uninstalled, id)
	}
	if touched {
		if serr := s.saveLedgerLocked(led); serr != nil {
			errs = append(errs, serr)
		}
	}
	return added, errors.Join(errs...)
}
