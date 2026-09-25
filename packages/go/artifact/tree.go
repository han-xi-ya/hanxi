package artifact

// ---------- 版本树：<root>/<entryName>_<version>/ 布局 ----------
//
// 事务纪律（PLAN §8.2）：
//   - staging（.tmp-<txn>）与最终目录同卷（同为 <root> 子目录），rename 原子落位；
//   - 已安装版本目录不可原地覆盖：同版本同 ZipSHA256 幂等复用，异摘要拒绝（防漂移）；
//   - 卸载先 rename 隔离到 .removing-<ts> 再删：文件被占用时留下可恢复状态，
//     删除不掉的残件由 CleanupAbandoned 在下次启动时收尸；
//   - 同一版本树并发互斥（按 root 共享进程内读写锁）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/versioncmp"
)

const (
	// tmpPrefix 解压中转目录前缀（StageDir 产物，扫描时忽略）。
	tmpPrefix = ".tmp-"
	// corruptPrefix 黑户隔离目录前缀（无可信账本的目标目录改名让位，永久保留取证，
	// 与 .tmp-/.removing- 一样对扫描/解析隐形；删除决策永远留给用户）。
	corruptPrefix = ".corrupt-"
	// removingMarker 卸载隔离标记（Remove 中途产物，扫描时忽略）。
	removingMarker = ".removing-"
	// metaFileName 落位元信息文件名。
	metaFileName = "meta.json"
	// DefaultSchema Meta.Schema 当前版本。
	DefaultSchema = 1
	// SourceRemote / SourceImported Meta.Source 取值。
	SourceRemote   = "remote"
	SourceImported = "imported"
)

// Meta 是落位元信息（meta.json）。字段命名兼容 litemonitor 既有账本
// （installedAt / zipSHA256 / assetSHA256 / source）；Source 取值 remote|imported。
type Meta struct {
	Schema      int       `json:"schema"`
	Tool        string    `json:"tool"`
	Version     string    `json:"version"`
	Entry       string    `json:"entry"`
	ZipSHA256   string    `json:"zipSHA256"`
	AssetSHA256 string    `json:"assetSHA256"`
	InstalledAt time.Time `json:"installedAt"`
	Source      string    `json:"source"`
}

// UnmarshalJSON 兼容历史 installedAt 两种写法：RFC3339（time.Time 原生）
// 与托管族旧账本的 "2006-01-02 15:04:05"（本地时区）。
// installedAt 走 RawMessage 中转，避免 time.Time 严格解析直接炸掉整份账本。
func (m *Meta) UnmarshalJSON(data []byte) error {
	type metaWire struct {
		Schema      int             `json:"schema"`
		Tool        string          `json:"tool"`
		Version     string          `json:"version"`
		Entry       string          `json:"entry"`
		ZipSHA256   string          `json:"zipSHA256"`
		AssetSHA256 string          `json:"assetSHA256"`
		InstalledAt json.RawMessage `json:"installedAt"`
		Source      string          `json:"source"`
	}
	var w metaWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	var installedAt time.Time
	if len(w.InstalledAt) > 0 && string(w.InstalledAt) != "null" {
		if err := json.Unmarshal(w.InstalledAt, &installedAt); err != nil {
			var s string
			if json.Unmarshal(w.InstalledAt, &s) == nil {
				if parsed, perr := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); perr == nil {
					installedAt = parsed
				}
			}
		}
	}
	*m = Meta{
		Schema:      w.Schema,
		Tool:        w.Tool,
		Version:     w.Version,
		Entry:       w.Entry,
		ZipSHA256:   w.ZipSHA256,
		AssetSHA256: w.AssetSHA256,
		InstalledAt: installedAt,
		Source:      w.Source,
	}
	return nil
}

// Version 是版本树扫描的一个条目。Meta 缺省零值表示账本缺失/不可读
// （Resolve 取最新时跳过此类残件目录）。
type Version struct {
	Version string `json:"version"`
	Dir     string `json:"dir"`
	Meta    Meta   `json:"meta"`
}

// treeLocks 按规范化 root 共享进程内读写锁（同 ocr hostedTreeLocks 手法）。
var treeLocks sync.Map // map[lower(cleanRoot)]*sync.RWMutex

func treeLockFor(root string) *sync.RWMutex {
	key := strings.ToLower(filepath.Clean(root))
	lock, _ := treeLocks.LoadOrStore(key, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

// Tree 版本树管理器：<root>/<entryName>_<version>/ 布局，同卷 staging。
// 构造纯本地无副作用；EntryName/root 非法时不 panic，错误延后到各方法返回。
type Tree struct {
	Root      string // 版本树根目录（如 <数据根>/versions/litemonitor）
	EntryName string // 目录名前缀（<entryName>_<version>）

	dirRe   *regexp.Regexp
	initErr error
	mu      *sync.RWMutex
}

// OpenTree 以版本树根目录与入口名前缀创建 Tree（不触盘）。
func OpenTree(root, entryName string) *Tree {
	t := &Tree{Root: filepath.Clean(strings.TrimSpace(root)), EntryName: entryName}
	switch {
	case t.Root == "" || t.Root == string(filepath.Separator):
		t.initErr = fmt.Errorf("版本树根目录无效: %q", root)
		return t
	}
	if err := ValidateVersionToken(entryName); err != nil {
		t.initErr = fmt.Errorf("托管入口名前缀无效: %w", err)
		return t
	}
	t.dirRe = regexp.MustCompile(`^` + regexp.QuoteMeta(entryName) + `_([A-Za-z0-9][A-Za-z0-9._-]{0,63})$`)
	t.mu = treeLockFor(t.Root)
	return t
}

// dirName 版本 → 托管目录名。
func (t *Tree) dirName(version string) string {
	return t.EntryName + "_" + version
}

// StageDir 创建独占中转目录 <root>/.tmp-<txnID>。txnID 过版本令牌白名单；
// 同名中转目录已存在即报错（独占语义）。discard 幂等清理（提交成功后为 no-op）。
func (t *Tree) StageDir(txnID string) (string, func(), error) {
	noop := func() {}
	if t.initErr != nil {
		return "", noop, t.initErr
	}
	if err := ValidateVersionToken(txnID); err != nil {
		return "", noop, fmt.Errorf("事务 ID 无效: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := os.MkdirAll(t.Root, 0755); err != nil {
		return "", noop, fmt.Errorf("创建版本树根目录失败: %w", err)
	}
	staging := filepath.Join(t.Root, tmpPrefix+txnID)
	if err := os.Mkdir(staging, 0755); err != nil {
		return "", noop, fmt.Errorf("创建中转目录失败（同名事务可能仍在进行）: %w", err)
	}
	return staging, func() { _ = os.RemoveAll(staging) }, nil
}

// Commit 将 staging 原子落位为 <root>/<entryName>_<version>/。
// 校验链：版本令牌白名单 → staging 归属与类型（Lstat 拒链接）→ 目标既有态检查
// （同版本同 ZipSHA256 幂等；异摘要拒绝防漂移；无可信账本的黑户目录改名
// .corrupt- 隔离后放行重装）→ meta 先在 staging 写全 → rename 一步定终身
// （P0 批 2a：Commit 原子边界收口于 rename，杜绝"rename 后写账前"崩溃黑户）。
func (t *Tree) Commit(staging, version string, meta Meta) error {
	if t.initErr != nil {
		return t.initErr
	}
	if err := ValidateVersionToken(version); err != nil {
		return err
	}
	if meta.ZipSHA256 != "" && !isSHA256Hex(meta.ZipSHA256) {
		return fmt.Errorf("meta.ZipSHA256 不是合法 sha256：%q", meta.ZipSHA256)
	}
	if meta.AssetSHA256 != "" && !isSHA256Hex(meta.AssetSHA256) {
		return fmt.Errorf("meta.AssetSHA256 不是合法 sha256：%q", meta.AssetSHA256)
	}
	if meta.Version != "" && meta.Version != version {
		return fmt.Errorf("meta.Version %q 与落位版本 %q 矛盾，拒收", meta.Version, version)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// staging 必须是版本树根下的普通目录（非链接），不得为空。
	staging = filepath.Clean(staging)
	rel, err := filepath.Rel(t.Root, staging)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("staging %q 不在版本树根目录内，拒绝落位（防任意目录搬运）", staging)
	}
	st, err := os.Lstat(staging)
	if err != nil {
		return fmt.Errorf("中转目录不可用: %w", err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("中转目录不是普通目录（拒绝链接占位）: %s", staging)
	}
	if ents, err := os.ReadDir(staging); err != nil {
		return fmt.Errorf("读取中转目录失败: %w", err)
	} else if len(ents) == 0 {
		return fmt.Errorf("中转目录为空，拒绝落位空版本: %s", staging)
	}

	target := filepath.Join(t.Root, t.dirName(version))
	if st, lerr := os.Lstat(target); lerr == nil {
		// 目标已存在：必须是普通目录且账本可信，否则拒绝覆盖。
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return fmt.Errorf("托管版本目标不是普通目录（拒绝链接/文件占位），拒绝覆盖: %s", target)
		}
		existing, merr := readTreeMeta(target)
		if merr != nil {
			// 无可信账本的伪残留（历史崩溃窗口的"有内容无 meta"黑户、手工拷入的
			// 外来目录）：改名隔离后放行重装——绝不静默覆盖，也绝不删除（数据主权
			// 留给用户，隔离目录因不匹配托管族命名而对扫描/解析隐形）。
			quarantine := filepath.Join(t.Root, corruptPrefix+filepath.Base(target)+"-"+time.Now().Format("20060102-150405"))
			if rerr := os.Rename(target, quarantine); rerr != nil {
				return fmt.Errorf("版本 %s 目录缺少可信元信息且隔离搬迁失败（拒绝覆盖以保数据）: %w", version, rerr)
			}
		} else {
			// 已有可信账本：同摘要幂等复用，异摘要拒绝（防同版本内容漂移）。
			if meta.ZipSHA256 != "" && strings.EqualFold(existing.ZipSHA256, meta.ZipSHA256) {
				_ = os.RemoveAll(staging)
				return nil
			}
			return fmt.Errorf("版本 %s 已安装，但本次包摘要与已装账本不同；为防止同版本内容漂移拒绝覆盖，请发布并使用新版本号", version)
		}
	} else if !os.IsNotExist(lerr) {
		return fmt.Errorf("检查现有版本目录失败: %w", lerr)
	}

	if err := os.MkdirAll(t.Root, 0755); err != nil {
		return fmt.Errorf("创建版本树根目录失败: %w", err)
	}
	// 补全账本缺省字段；meta 先在 staging 内完整写好，再整体 rename——
	// Commit 原子边界收口在 rename 一步（P0 批 2a，审查 §3.5：杜绝旧序
	// "先落位后写账"崩溃留下的"有内容无账本"黑户窗口）。账本写失败时
	// staging 原样未入位，无需回退逻辑，重试/背书清理均走既有通道。
	meta.Schema = DefaultSchema
	meta.Tool = firstNonEmpty(meta.Tool, t.EntryName)
	meta.Version = version
	meta.Source = firstNonEmpty(meta.Source, SourceRemote)
	if meta.InstalledAt.IsZero() {
		meta.InstalledAt = time.Now()
	}
	if err := writeJSONFile(filepath.Join(staging, metaFileName), meta); err != nil {
		return fmt.Errorf("写入版本元信息失败（staging 未落位，可直接重试）: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		return fmt.Errorf("版本目录落位失败: %w", err)
	}
	return nil
}

// Versions 扫描版本树并按版本号降序返回（最新在前）。
// 忽略 .tmp-/.removing- 事务目录；账本缺失/损坏的目录仍列出（Meta 零值），
// 供上层展示"异常安装"。
func (t *Tree) Versions() ([]Version, error) {
	if t.initErr != nil {
		return nil, t.initErr
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.scanLocked()
}

func (t *Tree) scanLocked() ([]Version, error) {
	entries, err := os.ReadDir(t.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 未安装任何版本
		}
		return nil, fmt.Errorf("扫描版本树失败: %w", err)
	}
	var out []Version
	for _, ent := range entries {
		name := ent.Name()
		if !ent.IsDir() || strings.HasPrefix(name, tmpPrefix) || strings.HasPrefix(name, corruptPrefix) || strings.Contains(name, removingMarker) {
			continue
		}
		g := t.dirRe.FindStringSubmatch(name)
		if g == nil {
			continue // 非本托管族目录：不干涉外来内容
		}
		version := g[1]
		dir := filepath.Join(t.Root, name)
		v := Version{Version: version, Dir: dir}
		if meta, err := readTreeMeta(dir); err == nil {
			v.Meta = meta
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return versioncmp.Compare(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// Resolve 定位版本目录。version 为空时返回"最新可用"（有可信账本的最新版）。
func (t *Tree) Resolve(version string) (string, error) {
	if t.initErr != nil {
		return "", t.initErr
	}
	version = strings.TrimSpace(version)
	if version == "" {
		t.mu.RLock()
		defer t.mu.RUnlock()
		list, err := t.scanLocked()
		if err != nil {
			return "", err
		}
		for _, v := range list { // 降序：第一个有可信账本者即可用最新
			if v.Meta.Schema != 0 {
				return v.Dir, nil
			}
		}
		return "", fmt.Errorf("版本树 %s 下没有可用的已安装版本", t.Root)
	}
	if err := ValidateVersionToken(version); err != nil {
		return "", err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	dir := filepath.Join(t.Root, t.dirName(version))
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("版本 %s 未安装（%s）", version, dir)
	}
	return dir, nil
}

// Remove 卸载指定版本：inUse 先行拒卸（上层用它探测进程占用），再 rename 隔离到
// .removing-<ts> 后删除。目录被锁定时返回"先退出"人话错误，不留半成品账本。
func (t *Tree) Remove(version string, inUse func(dir string) error) error {
	if t.initErr != nil {
		return t.initErr
	}
	if err := ValidateVersionToken(version); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	dir := filepath.Join(t.Root, t.dirName(version))
	st, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("版本未安装：%s（%v）", version, err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("托管版本目录不是普通目录，拒绝删除: %s", dir)
	}
	if inUse != nil {
		if err := inUse(dir); err != nil {
			return fmt.Errorf("无法卸载：该版本正在使用中，请先退出程序后重试（%v）", err)
		}
	}
	removing := dir + removingMarker + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.Rename(dir, removing); err != nil {
		return fmt.Errorf("无法卸载：相关文件可能正在被使用；请先退出该程序后重试: %w", err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("版本目录已隔离但未能清空残件（%s）；不影响解除登记，残件将在下次启动自动清理: %w", removing, err)
	}
	return nil
}

// CleanupAbandoned 启动恢复用：清理孤儿 .tmp-*/.removing-* 事务目录。
// 返回删除失败的残件路径（文件仍被占用），调用方可在下次启动或用户知情后重试。
func (t *Tree) CleanupAbandoned() []string {
	if t.initErr != nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := os.ReadDir(t.Root)
	if err != nil {
		return nil
	}
	var leftovers []string
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, tmpPrefix) || strings.Contains(name, removingMarker) {
			full := filepath.Join(t.Root, name)
			if err := os.RemoveAll(full); err != nil {
				leftovers = append(leftovers, full)
			}
		}
	}
	return leftovers
}

// readTreeMeta 读取并验证版本目录账本（schema 非零才算可信）。
func readTreeMeta(dir string) (Meta, error) {
	raw, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return Meta{}, err
	}
	if m.Schema == 0 {
		return Meta{}, fmt.Errorf("账本缺少 schema 字段（不可信）")
	}
	return m, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
