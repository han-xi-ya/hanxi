package clipboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hw "hanxi/internal/platform/windows"

	"hanxi/internal/jsonstore"
	"hanxi/internal/notify"
)

// 整库 index.json 权威存储（契约 §2）：加载/原子写、去重顶置、容量钳制与 LRU
// 淘汰、text 落盘 DPAPI 加解密、坏库隔离。内存态持明文（读出即解密，写盘前统一
// 加密），entries 恒为新→旧排序。
//
// 锁纪律：store 自带 mu 串行化全部读改写；服务层不再对 entries 加锁，事件在
// store 锁外由服务补发（store 只返回淘汰清单，不碰 Wails）。

const indexVersion = 1

// 容量钳制上限（契约 §2；v1.4.2 补 text 单条闸，与图片 16MiB 同谱双保险——
// MCP 装载侧另有密文预筛，此处是入库第一道）。
const (
	maxEntries    = 500
	maxBlobBytes  = int64(100) << 20
	maxImageBytes = int64(16) << 20
	maxTextBytes  = int64(8) << 20
)

// blobRelPrefix Entry.Blob 的库内相对前缀；A6 BlobStore 只认 "<sha>.png"，
// 出入库经 blobRelOf 换算。
const blobRelPrefix = "blobs/"

func blobRelOf(e Entry) string { return strings.TrimPrefix(e.Blob, blobRelPrefix) }

// blobIO A6 线 BlobStore 的最小依赖面（冻结签名见契约 §10，*BlobStore 天然
// 满足；服务/存储测试注入假件，不碰真实磁盘 blobs）。
type blobIO interface {
	SaveImage(img image.Image) (rel string, width, height int, err error)
	Load(rel string) ([]byte, error)
	Delete(rel string) error
	TotalBytes() (int64, error)
}

// sealFunc/openFunc text 加解密缝（默认 DPAPI；测试注入可逆假件验证调用链）。
type sealFunc func([]byte) (string, error)
type openFunc func(string) ([]byte, error)

// diskIndex index.json 落盘形态（text 字段为 base64(DPAPI) 密文）。
type diskIndex struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// diskIndexRaw 装载专用宽容形态（契约 §12 v1.2.3）：条目以 json.RawMessage
// 延迟解码，单条坏形态不连坐整库。
type diskIndexRaw struct {
	Version int               `json:"version"`
	Entries []json.RawMessage `json:"entries"`
}

// Store 剪贴板历史整库引擎。
type Store struct {
	dir      string // <DataDir>/clipboard
	path     string // index.json 绝对路径
	blobsDir string // blobs/ 绝对路径（ClearAll 全清用）
	blobs    blobIO

	seal sealFunc
	open openFunc

	mu      sync.Mutex
	entries []Entry // 新→旧，明文权威
}

// NewStore 建库并装载：目录缺失即创建；index.json 不存在按空库初始化；
// 信封级不可解析改名隔离取证副本后空库启动（memo 旧库同谱），单条坏形态
// 逐条跳过+保真隔离（契约 §12 v1.2.3）。seal/open 传 nil 用 DPAPI 默认实现。
//
// 无头表禁令钉注（契约 §12 v1.2.4，安全级）：**本构造函数有落盘副作用**
// （建目录 + 空库基线回写），系 GUI 侧刻意为之；因此本模块**禁止进入无头
// registry/mcpModules 懒激活**——懒实例化会毁掉 MCP 面"零落盘"承诺。启用门
// 照 memo 先例走 config.json 直读 enabled+receipt，由主控收口执行，此处只把
// 事实钉进注释。
//
// 同步红线（同族纪律，blobs.go 头注释有全量阐述）：text 明文落盘=DPAPI 密文、
// blobs 二进制=用户级加密**永不能跨机还原**，整个 clipboard 数据目录**必须
// 排除出 NAS/网盘同步**。
func NewStore(dir string, blobs blobIO, seal sealFunc, open openFunc) (*Store, error) {
	if seal == nil {
		seal = hw.DPAPIEncrypt
	}
	if open == nil {
		open = hw.DPAPIDecrypt
	}
	st := &Store{
		dir:      dir,
		path:     filepath.Join(dir, "index.json"),
		blobsDir: filepath.Join(dir, "blobs"),
		blobs:    blobs,
		seal:     seal,
		open:     open,
		entries:  []Entry{},
	}
	if err := os.MkdirAll(st.blobsDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建剪贴板 blobs 目录失败: %w", err)
	}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

// ---------- 装载与落盘 ----------

func (st *Store) load() error {
	var idx diskIndexRaw
	ok, err := jsonstore.Load(st.path, &idx)
	switch {
	case err == nil && !ok:
		// 文件不存在：空库起步，立刻落一份空 index 建立基线
		return st.saveLocked()
	case errors.Is(err, jsonstore.ErrEmpty):
		slog.Warn("clipboard: index.json 为 0 字节，按空库启动")
		st.entries = []Entry{}
		return st.saveLocked()
	case errors.Is(err, jsonstore.ErrCorrupt):
		// 信封级都解不开（整档残缺/非 JSON）：连坐改名隔离无从拆分，整档取证
		return st.quarantineAndReset(err)
	case err != nil:
		return fmt.Errorf("读取剪贴板历史失败: %w", err)
	}

	if idx.Version != indexVersion {
		// GUI 面宽容装载（契约 §12 v1.2.2 分面裁决：无头 MCP 面对 version≠1
		// fail-loud 拒载，GUI 面按现有字段尽量装载，两边口径差异系有意为之）
		slog.Warn("clipboard: index.json 版本与当前实现不符，按现有字段尽量装载（GUI 宽容口径）",
			"disk_version", idx.Version, "code_version", indexVersion)
	}

	loaded := make([]Entry, 0, len(idx.Entries))
	dropped := 0
	for i, raw := range idx.Entries {
		var e Entry
		if uerr := json.Unmarshal(raw, &e); uerr != nil {
			// 单条形态坏：跳过+warn+保真隔离原件（v1.2.3，不连坐整库）
			st.quarantineRawEntry(raw, i, uerr)
			dropped++
			continue
		}
		if e.Kind != KindText && e.Kind != KindImage && e.Kind != KindFile {
			st.quarantineRawEntry(raw, i, fmt.Errorf("未知条目类型 %q（前向未知形态）", e.Kind))
			dropped++
			continue
		}
		// text 非空即密文（图片条目 text 恒为空串不加密，契约 §2，自然跳过）
		if e.Text != "" {
			plain, derr := st.open(e.Text)
			if derr != nil {
				// 解密失败（换机器/换用户导致 DPAPI 域不符，或手工改库）：
				// 正文已不可还原，跳过+保真隔离（v1.2.3 同通道），保留会反复卡读写
				st.quarantineRawEntry(raw, i, fmt.Errorf("解密失败（正文不可还原）: %w", derr))
				dropped++
				continue
			}
			e.Text = string(plain)
		}
		e.BlobData = nil // 内存态不常驻图片字节，Get 时按需回读
		loaded = append(loaded, e)
	}
	st.entries = loaded
	if dropped > 0 {
		// 把剔除结果固化回盘，坏条目不反复触发解码/解密失败
		return st.saveLocked()
	}
	return nil
}

// quarantineRawEntry 把单条坏目的**原始字节**保真写入旁路取证副本
// （index.json.corrupt-<ts>#<序号>，memo 隔离改名同谱：内容一字节不动，
// 事后可手工捞回）。副本写入失败只 warn——隔离不成也不能连坐其余好条目。
func (st *Store) quarantineRawEntry(raw json.RawMessage, seq int, reason error) {
	dest := fmt.Sprintf("%s.corrupt-%s#%03d", st.path, time.Now().Format("20060102-150405"), seq)
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		slog.Warn("clipboard: 坏条目取证副本写入失败（该条仍跳过装载）", "err", err)
	} else {
		slog.Error("clipboard: 坏条目已跳过装载并保真隔离原件", "quarantined_to", dest, "reason", reason.Error())
	}
}

// quarantineAndReset 损坏库改名保留取证副本（index.json.corrupt-<ts>），
// 空库启动 + 通知中心红字；改名失败则拒绝覆盖可疑数据、报错上浮。
func (st *Store) quarantineAndReset(loadErr error) error {
	quarantine := st.path + ".corrupt-" + time.Now().Format("20060102-150405")
	if rerr := os.Rename(st.path, quarantine); rerr != nil {
		return fmt.Errorf("读取剪贴板历史失败且隔离改名失败（拒绝以空库覆盖可疑数据）: %v / %w", rerr, loadErr)
	}
	slog.Error("clipboard: 历史库损坏，已隔离取证副本并以空库启动（可从副本手工找回）",
		"err", loadErr, "quarantined_to", quarantine)
	notify.Error(ID, "剪贴板历史损坏已隔离",
		"index.json 解析失败，已改名保留取证副本，本次以空库启动", "/ext/clipboard")
	st.entries = []Entry{}
	return st.saveLocked()
}

// saveLocked 整库原子写（调用方持 st.mu）：text 逐条加密后 jsonstore.Save。
// 加密失败即报错不写盘——宁可历史不落盘，绝不把明文写进 index.json。
func (st *Store) saveLocked() error {
	disk := diskIndex{Version: indexVersion, Entries: make([]Entry, 0, len(st.entries))}
	for _, e := range st.entries {
		d := e
		d.BlobData = nil
		if d.Text != "" {
			sealed, err := st.seal([]byte(d.Text))
			if err != nil {
				return fmt.Errorf("加密条目文本失败（未落盘，历史保持原样）: %w", err)
			}
			d.Text = sealed
		}
		disk.Entries = append(disk.Entries, d)
	}
	if err := jsonstore.Save(st.path, disk); err != nil {
		return fmt.Errorf("保存剪贴板历史失败: %w", err)
	}
	return nil
}

// ---------- 读改写操作（全部自带锁，返回拷贝） ----------

// List 过滤检索（契约 §4）：q 小写子串匹配 Preview/SourceApp/Files/AutoTags
// 拼接串；kind 空或 "all" 不过滤；limit 钳到 ≤500（≤0 视为不限量=500）。
// 出口剥 Text/BlobData（列表页不需要正文）。
func (st *Store) List(q, kind string, limit int) []Entry {
	st.mu.Lock()
	defer st.mu.Unlock()

	if limit <= 0 || limit > maxEntries {
		limit = maxEntries
	}
	kw := strings.ToLower(strings.TrimSpace(q))
	out := make([]Entry, 0, len(st.entries))
	for _, e := range st.entries {
		if kind != "" && kind != "all" && string(e.Kind) != kind {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(e.Preview + "\n" + e.SourceApp + "\n" +
				strings.Join(e.Files, "\n") + "\n" + strings.Join(e.AutoTags, " "))
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, cloneEntry(e, true))
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Get 取单条完整明文拷贝（不记使用；记使用见 Touch）。
func (st *Store) Get(id string) (Entry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, e := range st.entries {
		if e.ID == id {
			return cloneEntry(e, false), true
		}
	}
	return Entry{}, false
}

// FindHash 按语义哈希查（服务层据此跳过重复截图的解码与落盘）。
func (st *Store) FindHash(hash string) (Entry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, e := range st.entries {
		if e.Hash == hash {
			return cloneEntry(e, false), true
		}
	}
	return Entry{}, false
}

// commitLocked 提交式改账（调用方持锁）：mutate 先改内存，随即整库加密原子
// 落盘；落盘失败回滚内存并如实上浮错误——"内存与盘不分叉"只此一条通路，
// 各写操作禁止散手序。prev 浅拷贝即安全：所有变更都是整条值替换，不原地
// 改 Entry 内共享切片。
func (st *Store) commitLocked(mutate func()) error {
	prev := append([]Entry(nil), st.entries...)
	mutate()
	if err := st.saveLocked(); err != nil {
		st.entries = prev
		return err
	}
	return nil
}

// Touch 记一次使用（useCount++/lastUsedAt=at），提交序见 commitLocked。
func (st *Store) Touch(id string, at time.Time) (Entry, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	idx := -1
	for i, e := range st.entries {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Entry{}, fmt.Errorf("剪贴板条目不存在: %s", id)
	}
	var candidate Entry
	if err := st.commitLocked(func() {
		candidate = st.entries[idx]
		candidate.UseCount++
		candidate.LastUsedAt = at.UnixMilli()
		st.entries[idx] = candidate
	}); err != nil {
		return Entry{}, err
	}
	return cloneEntry(candidate, false), nil
}

// Add 入库（服务层已完成装配：ID/Hash/Preview/AutoTags/Sensitive 就绪）。
// 语义见契约 §2：命中同 hash 同 Kind → 顶置 + useCount++/lastUsedAt 刷新
// （pinned 保留、其余字段维持原条目），不新增；新条目插顶后执行计数与 blob
// 预算淘汰。返回最终条目与被淘汰条目清单（调用方逐个发 clipboard:removed）。
func (st *Store) Add(e Entry, at time.Time) (Entry, []Entry, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	for i, ex := range st.entries {
		if ex.Hash != e.Hash || ex.Kind != e.Kind {
			continue
		}
		var moved Entry
		err := st.commitLocked(func() {
			moved = ex
			moved.UseCount++
			moved.LastUsedAt = at.UnixMilli()
			rest := make([]Entry, 0, len(st.entries))
			rest = append(rest, moved)
			rest = append(rest, st.entries[:i]...)
			rest = append(rest, st.entries[i+1:]...)
			st.entries = rest
		})
		if err != nil {
			return Entry{}, nil, err
		}
		return cloneEntry(moved, false), nil, nil
	}

	e.BlobData = nil
	var evicted []Entry
	if err := st.commitLocked(func() {
		st.entries = append([]Entry{e}, st.entries...)
		evicted = st.evictLocked()
	}); err != nil {
		return Entry{}, nil, err
	}
	return cloneEntry(e, false), evicted, nil
}

// evictLocked 钳制淘汰（调用方持锁）：先条数超 500 按 LRU 从尾裁（pinned/manual
// 豁免），再 blobs 总量超 100MiB 同序裁图片条目并删其 blob。全库皆
// pinned/manual 时允许超限（如实保留，绝不误杀用户钉选）。
func (st *Store) evictLocked() []Entry {
	var evicted []Entry

	for len(st.entries) > maxEntries {
		idx := st.pickEvictIndex("")
		if idx < 0 {
			break
		}
		victim := st.entries[idx]
		st.entries = append(st.entries[:idx], st.entries[idx+1:]...)
		st.releaseBlob(victim)
		evicted = append(evicted, victim)
	}

	total, err := st.blobs.TotalBytes()
	if err != nil {
		slog.Warn("clipboard: blobs 总量读取失败，跳过体积淘汰（条数钳制不受影响）", "err", err)
		return evicted
	}
	for total > maxBlobBytes {
		idx := st.pickEvictIndex(KindImage)
		if idx < 0 {
			slog.Warn("clipboard: blobs 总量超限但无可淘汰图片条目（全部钉选/固定），如实保留",
				"blob_bytes", total)
			break
		}
		victim := st.entries[idx]
		st.entries = append(st.entries[:idx], st.entries[idx+1:]...)
		st.releaseBlob(victim)
		evicted = append(evicted, victim)
		next, terr := st.blobs.TotalBytes()
		if terr != nil {
			slog.Warn("clipboard: 体积淘汰后总量复测失败，停止本回淘汰", "err", terr)
			break
		}
		total = next
	}
	return evicted
}

// pickEvictIndex 挑选淘汰位：entries 新→旧，自尾向前取第一个非 pinned 非
// manual（wantKind 非空时还须是该 Kind 且带 Blob）；LRU 语义由"尾=最久未动 +
// 触用/重复制即顶置"维持。无候选返回 -1。
func (st *Store) pickEvictIndex(wantKind Kind) int {
	for i := len(st.entries) - 1; i >= 0; i-- {
		e := st.entries[i]
		if e.Pinned || e.Manual {
			continue
		}
		if wantKind != "" && (e.Kind != wantKind || e.Blob == "") {
			continue
		}
		return i
	}
	return -1
}

// releaseBlob 删除条目的 blob（淘汰路径尽力而为：失败只 warn，不回滚条目删除——
// 索引已不含它，孤 blob 由 ClearAll/下次预算复测收口）。
func (st *Store) releaseBlob(e Entry) {
	if e.Blob == "" {
		return
	}
	if err := st.blobs.Delete(blobRelOf(e)); err != nil {
		slog.Warn("clipboard: 淘汰条目的 blob 删除失败（留孤儿，全删时收口）",
			"blob", e.Blob, "err", err)
	}
}

// TogglePin 翻转钉选，候选先落盘再换装。
func (st *Store) TogglePin(id string) (Entry, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	idx := -1
	for i, e := range st.entries {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Entry{}, fmt.Errorf("剪贴板条目不存在: %s", id)
	}
	var candidate Entry
	if err := st.commitLocked(func() {
		candidate = st.entries[idx]
		candidate.Pinned = !candidate.Pinned
		st.entries[idx] = candidate
	}); err != nil {
		return Entry{}, err
	}
	return cloneEntry(candidate, false), nil
}

// Delete 删单条：带 blob 的先删 blob——失败即报错且不删条目（内存与盘不分叉），
// 成功后整库原子写回。出口拷贝剥 Text/BlobData（removed 事件只需 id，调用方自取）。
func (st *Store) Delete(id string) (Entry, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	idx := -1
	for i, e := range st.entries {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Entry{}, fmt.Errorf("剪贴板条目不存在: %s", id)
	}
	e := st.entries[idx]
	if e.Blob != "" {
		// 先删 blob：失败即报错且不删条目（内存与盘不分叉）
		if err := st.blobs.Delete(blobRelOf(e)); err != nil {
			return Entry{}, fmt.Errorf("删除图片内容失败（条目保持原样）: %w", err)
		}
	}
	if err := st.commitLocked(func() {
		st.entries = append(st.entries[:idx], st.entries[idx+1:]...)
	}); err != nil {
		return Entry{}, err
	}
	return cloneEntry(e, true), nil
}

// ClearAll 历史与 blobs 全清：先清 blobs 目录（含孤儿）再写回空库，任一步失败
// 聚合如实上浮（可重试；内存仅在盘写成功后才换装）。
func (st *Store) ClearAll() error {
	st.mu.Lock()
	defer st.mu.Unlock()

	var errs []error
	if err := os.RemoveAll(st.blobsDir); err != nil {
		errs = append(errs, fmt.Errorf("清空 blobs 目录失败: %w", err))
	} else if err := os.MkdirAll(st.blobsDir, 0o755); err != nil {
		errs = append(errs, fmt.Errorf("重建 blobs 目录失败: %w", err))
	}

	prev := st.entries
	st.entries = []Entry{}
	if err := st.saveLocked(); err != nil {
		st.entries = prev // 空库未落稳，账本回滚，条目与盘对齐
		if len(errs) == 0 {
			// blobs 已清而索引未写回：不一致态如实报出，交重试收敛
			return fmt.Errorf("历史索引写回失败而 blobs 已清空（条目仍在库中但图片内容已丢失，请重试全删）: %w", err)
		}
		return errors.Join(append([]error{err}, errs...)...)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Count 当前条目数（GetStatus 用）。
func (st *Store) Count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.entries)
}

// BlobBytes blobs 总量（GetStatus 用；读取失败如实上浮）。
func (st *Store) BlobBytes() (int64, error) {
	return st.blobs.TotalBytes()
}
