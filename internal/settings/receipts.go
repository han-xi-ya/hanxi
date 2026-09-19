// receipts.go 逻辑安装凭据（receipt）的目录文件存储（Wave 1 后端底座，
// 存储契约冻结于 internal/extapi/receipt.go）。
//
// 布局：<dir>/<moduleId>.json 每模块一份，内容即 extapi.Receipt JSON。
// 读取纪律：磁盘缺失 = 未安装；损坏文件按未安装处理但 slog.Warn 留痕——
// Registry 投影不得被个别坏文件阻断。写入纪律：复用 writeAtomicFile
// （tmp+rename，对齐 Store.saveLocked），写失败旧凭据原样保留，错误中文可诊断。
// 安全纪律：moduleId 直接参与文件名拼接，必须先过消毒闸门（仅 [a-z0-9-]），
// 拒绝路径分隔、上级跳出与空名，防路径注入。
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
// 无内存缓存：凭据是低频小文件，读取直达磁盘即可，磁盘保持单一事实源
// （安装态的批量投影由 Registry 侧在内存汇总，不归本存储管）。
// mu 为 RWMutex：写入侧（MarkInstalled/MarkAbsent）持写锁护住
// "读-判-写" 序列——幂等判定与删除若被并发穿插，会交错出形态漂移或
// 删后复活的孤儿文件；读取侧（IsInstalled/Installed）持读锁与写侧互斥，
// 保证投影内不混用同一模块的改前/改后态。
type ReceiptStore struct {
	dir string
	mu  sync.RWMutex
}

// NewReceiptStore 创建指向 receiptsDir 的凭据存储。
// 目录预建归数据根布局 ensureDirs（见 paths.go）；构造函数不做任何 IO，
// 测试可直接注入 t.TempDir()。
func NewReceiptStore(receiptsDir string) *ReceiptStore {
	return &ReceiptStore{dir: receiptsDir}
}

// sanitizeModuleID moduleId 消毒闸门：仅允许小写字母、数字与连字符且非空。
// 该字符集天然排除路径分隔符、"."、".."、盘符与空字节，通过校验即可安全拼名。
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
	return s.markInstalledLocked(moduleID, kind)
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

// MarkAbsent 移除凭据（逻辑卸载）。文件不存在视为已卸载（幂等）；
// 模块数据目录不在凭据职责内，此处绝不删除。
func (s *ReceiptStore) MarkAbsent(moduleID string) error {
	if err := sanitizeModuleID(moduleID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

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

// knownLedgerFile 是 receipts 目录内的"已见模块"名单（非凭据文件，故 sanitize
// 校验不过它的文件名也无妨——它不参与 IsInstalled 判定）。
const knownLedgerFile = "known-modules.json"

// knownLedger state 落盘形状：登记过的模块 ID 定序清单。迁移记忆由本名单承载
// 而非凭据本身——否则"每次启动全量补建"会让用户卸载的模块重启后复活。
type knownLedger struct {
	Schema    int      `json:"schema"`
	ModuleIDs []string `json:"moduleIds"`
}

// EnsureSeen 以当前注册模块全集驱动一次性迁移与增量安装：
// 名单内模块绝不补建（卸载永久生效）；名单外"新看见"的模块补建 builtin-logical
// 凭据并计入名单——首次运行名单为空,即等价全量迁移（Enabled=true→installed+enabled、
// Enabled=false→installed+disabled,ADR-0001 §1.5）；此后版本升级新增的模块自动安装。
// 名单缺失/损坏按空名单起步（一次性把现存模块重新认全,不丢数据;边界:若恰有
// 已卸载模块会获一次重建,可再次卸载）。返回本轮新看见的模块 ID 列表。
func (s *ReceiptStore) EnsureSeen(registeredIDs []string, kind extapi.ReceiptKind) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, knownLedgerFile)
	known := map[string]bool{}
	if raw, err := os.ReadFile(path); err == nil {
		var led knownLedger
		if json.Unmarshal(raw, &led) == nil && led.Schema == extapi.ModuleContractSchema {
			for _, id := range led.ModuleIDs {
				known[id] = true
			}
		} else {
			slog.Warn("安装名单不可解析，按空名单起步（现存模块将重新认全）", "path", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("安装名单读取异常，按空名单起步", "path", path, "err", err)
	}

	var errs []error
	var added []string
	for _, id := range registeredIDs {
		if known[id] {
			continue
		}
		if err := sanitizeModuleID(id); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.markInstalledLocked(id, kind); err != nil {
			errs = append(errs, err)
			continue // 失败的模块不入名单，下次启动重试
		}
		known[id] = true
		added = append(added, id)
	}

	if len(added) > 0 {
		ids := make([]string, 0, len(known))
		for id := range known {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		raw, err := json.MarshalIndent(knownLedger{Schema: extapi.ModuleContractSchema, ModuleIDs: ids}, "", "  ")
		if err != nil {
			return added, errors.Join(append(errs, err)...)
		}
		if err := writeAtomicFile(path, raw); err != nil {
			errs = append(errs, fmt.Errorf("写入安装名单失败: %w", err))
		}
	}
	return added, errors.Join(errs...)
}
