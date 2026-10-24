package clipboard

// 剪贴板模块用户偏好（借鉴 TieZ 三功能轮，契约 §12 v1.8）：容量可调、来源应用
// 策略（排除表自定义/按应用保留天数）、粘贴后即删、队列键位、预览遮蔽规则，
// 独立落 <StateDir>/clipboard-prefs.json。
//
// 载体裁决（memo-prefs/ocr 同谱）：绝不掺进 index.json 数据本体——配置混进
// 账本会污染 ClearAll 的"不留残片"收口；也不进 config.json（数据根特征签名，
// ARCHITECTURE.md 钉死）。**构造只读不落盘**：缺文件/坏文件一律回落默认，
// 只有用户显式 SetPrefs 才产生第一份文件（NewStore 构造落盘副作用的镜像纪律：
// 偏好面连那份基线写都不必有）。
//
// 形态语义：磁盘全指针 omitempty 区分"未设置"（回落默认）与"显式值"；运行时
// 全规范化值（越界钳制、exe 补 .exe、去重），坏形态（非法正则/超数量闸）
// **拒绝上浮**——"以为设了其实没设"是安全错觉。nil 接收者全量回落默认
// （测试手装体不接 prefs 的路径天然安全，memo quickMemoStore 同款）。

import (
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

// 容量默认值与钳制区间（原 store.go 硬编码常量降级为默认值，契约 §12 v1.8）。
// 条数上限定 2000：saveLocked 是整库重写且 thumb 内嵌 index.json，条目越多
// 每次提交越贵——自用场景 2000 已是"很久不淘汰"，更高数值收益递减风险递增。
const (
	defaultMaxEntries = 500
	minMaxEntries     = 50
	maxMaxEntries     = 2000
	defaultMaxBlobMB  = 100
	// defaultMaxBlobBytes MB 默认值的字节形（store 初始化与测试对拍共用）。
	defaultMaxBlobBytes   = int64(defaultMaxBlobMB) << 20
	minMaxBlobMB          = 10
	maxMaxBlobMB          = 1024
	retentionMinDays      = 1
	retentionMaxDays      = 365
	maxRetentionRules     = 50  // 按应用保留规则数量闸
	maxExcludedCustom     = 100 // 自定义排除表数量闸
	maxMaskCustomPatterns = 50  // 自定义遮蔽正则数量闸
)

// 内置遮蔽 kind 全集（顺序即回显顺序）与出厂默认全开集。
// phone/idcard/email/secret 语义见 mask.go；未知 kind 静默丢弃（前向兼容）。
var (
	allMaskKinds     = []string{"phone", "idcard", "email", "secret"}
	defaultMaskKinds = append([]string(nil), allMaskKinds...)
)

// Prefs 服务绑定面回显/写入形态（契约 §12 v1.8 冻结 tag）。
// ExcludedFactory 只读回显（出厂 7 项结构上删不掉）；QueueHotkey 的**写路径
// 不在本结构语义内**——SetPrefs 忽略该字段保留旧值，改键只走 SetQueueHotkey
// （"校验即绑定、败不落盘"事务在热键域，避免配置存了键没绑上的分叉态）。
type Prefs struct {
	MaxEntries         int            `json:"maxEntries"`
	MaxBlobMB          int            `json:"maxBlobMB"`
	DeleteAfterPaste   bool           `json:"deleteAfterPaste"`
	ExcludedFactory    []string       `json:"excludedFactory"`
	ExcludedCustom     []string       `json:"excludedCustom"`
	AppRetentionDays   map[string]int `json:"appRetentionDays"`
	QueueHotkey        string         `json:"queueHotkey"`
	MaskKinds          []string       `json:"maskKinds"`
	MaskCustomPatterns []string       `json:"maskCustomPatterns"`
}

// clipPrefs 运行时规范化偏好（全值语义，未设置项已回落默认）。
type clipPrefs struct {
	MaxEntries         int
	MaxBlobMB          int
	DeleteAfterPaste   bool
	ExcludedCustom     []string
	AppRetentionDays   map[string]int
	QueueHotkey        string
	MaskKinds          []string
	MaskCustomPatterns []string
}

// persistedClipPrefs 磁盘形态：全指针 omitempty——落过盘才写，未落盘的项
// 加载回落默认（与"未设置 vs 显式空"的指针语义配套）。
type persistedClipPrefs struct {
	MaxEntries         *int            `json:"maxEntries,omitempty"`
	MaxBlobMB          *int            `json:"maxBlobMB,omitempty"`
	DeleteAfterPaste   *bool           `json:"deleteAfterPaste,omitempty"`
	ExcludedCustom     *[]string       `json:"excludedCustom,omitempty"`
	AppRetentionDays   *map[string]int `json:"appRetentionDays,omitempty"`
	QueueHotkey        *string         `json:"queueHotkey,omitempty"`
	MaskKinds          *[]string       `json:"maskKinds,omitempty"`
	MaskCustomPatterns *[]string       `json:"maskCustomPatterns,omitempty"`
}

// defaultClipPrefs 出厂运行态（遮蔽默认全开——隐私默认收紧是自用机正当姿势，
// 与排除表出厂 7 项不可裁撤同谱）。
func defaultClipPrefs() clipPrefs {
	return clipPrefs{
		MaxEntries:       defaultMaxEntries,
		MaxBlobMB:        defaultMaxBlobMB,
		AppRetentionDays: map[string]int{},
		MaskKinds:        append([]string(nil), defaultMaskKinds...),
	}
}

// normalizeClipPrefs 校验+规范化纯函数（SetPrefs 与装载共用一条路，口径唯一）：
//   - 数值越界静默钳制，0 视为"未设置"回落默认（GetPrefs 读回即规范值，如实回显）；
//   - 格式非法（坏正则/超数量闸）报错拒绝，整表不动；
//   - exe 键统一 trim+lower，无点后缀自动补 .exe（免用户猜）；
//   - MaskKinds：nil=全开默认，显式空表=全关（前端总发全量，两态可辨）。
func normalizeClipPrefs(in Prefs) (clipPrefs, error) {
	p := defaultClipPrefs()

	if in.MaxEntries != 0 {
		p.MaxEntries = clampInt(in.MaxEntries, minMaxEntries, maxMaxEntries)
	}
	if in.MaxBlobMB != 0 {
		p.MaxBlobMB = clampInt(in.MaxBlobMB, minMaxBlobMB, maxMaxBlobMB)
	}
	p.DeleteAfterPaste = in.DeleteAfterPaste

	custom, err := normalizeExeList(in.ExcludedCustom, maxExcludedCustom, "自定义排除表")
	if err != nil {
		return clipPrefs{}, err
	}
	// 出厂项并入无意义（活表本就是并集），规范化直接剔掉，回显只剩真增量。
	custom = excludeStrings(custom, sensitiveDefaultUsers)
	if custom == nil {
		custom = []string{}
	}
	p.ExcludedCustom = custom

	days := map[string]int{}
	if len(in.AppRetentionDays) > maxRetentionRules {
		return clipPrefs{}, fmt.Errorf("按应用保留规则超过上限 %d 项", maxRetentionRules)
	}
	for raw, d := range in.AppRetentionDays {
		key := normalizeExeKey(raw)
		if key == "" || d <= 0 {
			continue // 空键与"天数≤0"都按"该规则删除"处理
		}
		days[key] = clampInt(d, retentionMinDays, retentionMaxDays)
	}
	p.AppRetentionDays = days

	p.QueueHotkey = strings.TrimSpace(in.QueueHotkey)

	if in.MaskKinds == nil {
		p.MaskKinds = append([]string(nil), defaultMaskKinds...)
	} else {
		kept := []string{}               // 显式空表=全关：保持非 nil 与"未设置"可辨（回显/再存盘不翻回全开）
		for _, k := range allMaskKinds { // 固定序输出，未知 kind 静默丢弃
			for _, want := range in.MaskKinds {
				if strings.EqualFold(strings.TrimSpace(want), k) {
					kept = append(kept, k)
					break
				}
			}
		}
		p.MaskKinds = kept
	}

	patterns := make([]string, 0, len(in.MaskCustomPatterns))
	for _, raw := range in.MaskCustomPatterns {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, err := regexp.Compile(s); err != nil {
			return clipPrefs{}, fmt.Errorf("自定义遮蔽规则 %q 不是合法正则：%v", s, err)
		}
		patterns = appendUnique(patterns, s)
	}
	if len(patterns) > maxMaskCustomPatterns {
		return clipPrefs{}, fmt.Errorf("自定义遮蔽规则超过上限 %d 条", maxMaskCustomPatterns)
	}
	p.MaskCustomPatterns = patterns
	return p, nil
}

// normalizeExeKey 排除表/保留表 exe 键规范化：小写、去空白、无点后缀补 .exe。
// 与 listener 侧 processExeName（小写带 .exe 无路径）同谱对齐。
func normalizeExeKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if !strings.Contains(s, ".") {
		s += ".exe"
	}
	return s
}

// normalizeExeList 批量规范化+去重+数量闸，返回 nil 表示显式空表。
func normalizeExeList(items []string, capN int, what string) ([]string, error) {
	var out []string
	for _, raw := range items {
		s := normalizeExeKey(raw)
		if s == "" {
			continue
		}
		out = appendUnique(out, s)
	}
	if len(out) > capN {
		return nil, fmt.Errorf("%s超过上限 %d 项", what, capN)
	}
	return out, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// copyStrings 保 nil 语义的拷贝：nil 进 nil 出，非 nil（含空表）出非 nil。
func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s))
	return append(out, s...)
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

func excludeStrings(list, drop []string) []string {
	var out []string
	for _, s := range list {
		hit := false
		for _, d := range drop {
			if strings.EqualFold(s, d) {
				hit = true
				break
			}
		}
		if !hit {
			out = append(out, s)
		}
	}
	return out
}

// mergeExcludedTables 活表装配唯一通路：出厂 7 项 ∪ custom（去重，出厂在前）。
// password 子串硬规则不在此表——它钉在 matchExcluded 纯函数层，与表无关不可删。
func mergeExcludedTables(custom []string) []string {
	merged := DefaultExcludedExes()
	for _, c := range custom {
		merged = appendUnique(merged, strings.ToLower(strings.TrimSpace(c)))
	}
	return merged
}

// clipPrefsStore 偏好持久化。nil 接收者全量回落默认（service_test 手装体纪律）。
type clipPrefsStore struct {
	filePath string
	mu       sync.RWMutex
	prefs    clipPrefs
}

// newClipPrefsStore 只读装载：缺文件/坏文件回落默认且**不落盘**（构造零写盘）。
// 磁盘上的旧值同样过 normalize——手改文件/版本降级的坏形态收敛到合法域，
// 唯坏正则这种"格式非法"回落为丢弃该条（装载面不报错：报错没人接，
// 运行面拒绝即可）。
func newClipPrefsStore(dir string) *clipPrefsStore {
	s := &clipPrefsStore{filePath: filepath.Join(dir, "clipboard-prefs.json")}
	s.prefs = defaultClipPrefs()

	var pc persistedClipPrefs
	ok, err := jsonstore.Load(s.filePath, &pc)
	if !ok || err != nil {
		if err != nil {
			slog.Warn("clipboard: 偏好文件读取失败，按默认值运行", "err", err)
		}
		return s
	}
	p := normalizePersisted(pc)
	s.prefs = p
	return s
}

// normalizePersisted 磁盘形态 → 运行时形态：借用 normalizeClipPrefs 单路口径，
// 装载面遇到"拒绝级"坏形态（坏正则/超闸）时逐项剔除而不是整档作废——
// 剔除动作在回显（GetPrefs/Snapshot）里如实可见，用户能看到生效集。
func normalizePersisted(pc persistedClipPrefs) clipPrefs {
	in := Prefs{}
	if pc.MaxEntries != nil {
		in.MaxEntries = *pc.MaxEntries
	}
	if pc.MaxBlobMB != nil {
		in.MaxBlobMB = *pc.MaxBlobMB
	}
	if pc.DeleteAfterPaste != nil {
		in.DeleteAfterPaste = *pc.DeleteAfterPaste
	}
	if pc.ExcludedCustom != nil {
		in.ExcludedCustom = *pc.ExcludedCustom
	}
	if pc.AppRetentionDays != nil {
		in.AppRetentionDays = *pc.AppRetentionDays
	}
	if pc.QueueHotkey != nil {
		in.QueueHotkey = *pc.QueueHotkey
	}
	if pc.MaskKinds != nil {
		in.MaskKinds = *pc.MaskKinds
	}
	if pc.MaskCustomPatterns != nil {
		in.MaskCustomPatterns = *pc.MaskCustomPatterns
	}
	p, err := normalizeClipPrefs(in)
	if err == nil {
		return p
	}
	// 拒绝级坏形态（当前只可能是坏正则/超闸）：剔正则域重试，其余照常收敛。
	// 数量闸超限同样走这条路（normalize 报第一个错），MaskCustomPatterns 清空
	// 后重试一次；仍败则整档回落默认（宁可默认，不可带病运行）。
	in.MaskCustomPatterns = nil
	if p2, err2 := normalizeClipPrefs(in); err2 == nil {
		slog.Warn("clipboard: 偏好文件含非法遮蔽规则，已剔除后装载", "err", err)
		return p2
	}
	slog.Warn("clipboard: 偏好文件形态非法，整档回落默认", "err", err)
	return defaultClipPrefs()
}

// persist 运行时形态 → 磁盘形态（全指针非 nil：落过盘即全量显式化）。
func persistClipPrefs(p clipPrefs) persistedClipPrefs {
	if p.AppRetentionDays == nil {
		p.AppRetentionDays = map[string]int{}
	}
	if p.MaskKinds == nil {
		p.MaskKinds = []string{}
	}
	if p.MaskCustomPatterns == nil {
		p.MaskCustomPatterns = []string{}
	}
	if p.ExcludedCustom == nil {
		p.ExcludedCustom = []string{}
	}
	e, c := p.DeleteAfterPaste, p.QueueHotkey
	m, b := p.MaxEntries, p.MaxBlobMB
	kinds, pats, excl := p.MaskKinds, p.MaskCustomPatterns, p.ExcludedCustom
	days := p.AppRetentionDays
	return persistedClipPrefs{
		MaxEntries: &m, MaxBlobMB: &b, DeleteAfterPaste: &e,
		ExcludedCustom: &excl, AppRetentionDays: &days, QueueHotkey: &c,
		MaskKinds: &kinds, MaskCustomPatterns: &pats,
	}
}

// Set 校验并原子落盘，成功后换内存；失败内存保持旧值（与 store 提交式改账
// 同谱："内存与盘不分叉"）。
func (s *clipPrefsStore) Set(in Prefs) (clipPrefs, error) {
	if s == nil {
		return clipPrefs{}, fmt.Errorf("剪贴板偏好存储未就绪")
	}
	p, err := normalizeClipPrefs(in)
	if err != nil {
		return clipPrefs{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := jsonstore.Save(s.filePath, persistClipPrefs(p)); err != nil {
		return clipPrefs{}, fmt.Errorf("保存剪贴板偏好失败: %w", err)
	}
	s.prefs = p
	return p, nil
}

// ---------- 只读访问器（nil 接收者回落默认） ----------

func (s *clipPrefsStore) snapshot() clipPrefs {
	if s == nil {
		return defaultClipPrefs()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.prefs
	// 切片/映射拷贝隔离：调用方改副本不伤运行态；copyStrings 保住
	// "nil=未设置 vs 空表=显式全关"的可辨性（append(nil,..) 会把空表折叠成 nil）。
	p.ExcludedCustom = copyStrings(p.ExcludedCustom)
	p.MaskKinds = copyStrings(p.MaskKinds)
	p.MaskCustomPatterns = copyStrings(p.MaskCustomPatterns)
	if p.AppRetentionDays != nil {
		days := make(map[string]int, len(p.AppRetentionDays))
		maps.Copy(days, p.AppRetentionDays)
		p.AppRetentionDays = days
	}
	return p
}

// maxEntries/maxBlobBytes 生效容量闸（store 字段化限值的供数口）。
func (s *clipPrefsStore) maxEntries() int { return s.snapshot().MaxEntries }

func (s *clipPrefsStore) maxBlobBytes() int64 {
	return int64(s.snapshot().MaxBlobMB) << 20
}

func (s *clipPrefsStore) deleteAfterPaste() bool { return s.snapshot().DeleteAfterPaste }

func (s *clipPrefsStore) excludedCustom() []string { return s.snapshot().ExcludedCustom }

func (s *clipPrefsStore) appRetentionDays() map[string]int { return s.snapshot().AppRetentionDays }

func (s *clipPrefsStore) queueHotkey() string { return s.snapshot().QueueHotkey }

// setQueueHotkey 热键域专用落笔（SetQueueHotkey 校验即绑定成功后调；
// 与 SetPrefs 的整表写分道，见 Prefs 头注）。
func (s *clipPrefsStore) setQueueHotkey(accel string) error {
	if s == nil {
		return fmt.Errorf("剪贴板偏好存储未就绪")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prefs
	p.QueueHotkey = strings.TrimSpace(accel)
	if err := jsonstore.Save(s.filePath, persistClipPrefs(p)); err != nil {
		return fmt.Errorf("保存剪贴板队列热键失败: %w", err)
	}
	s.prefs = p
	return nil
}

func (s *clipPrefsStore) maskKinds() []string { return s.snapshot().MaskKinds }

func (s *clipPrefsStore) maskCustomPatterns() []string {
	return s.snapshot().MaskCustomPatterns
}

// wirePrefs 绑定面回显形态（含出厂表只读域与生效值）。
func (s *clipPrefsStore) wirePrefs() Prefs {
	p := s.snapshot()
	return Prefs{
		MaxEntries:         p.MaxEntries,
		MaxBlobMB:          p.MaxBlobMB,
		DeleteAfterPaste:   p.DeleteAfterPaste,
		ExcludedFactory:    DefaultExcludedExes(),
		ExcludedCustom:     p.ExcludedCustom,
		AppRetentionDays:   p.AppRetentionDays,
		QueueHotkey:        p.QueueHotkey,
		MaskKinds:          p.MaskKinds,
		MaskCustomPatterns: p.MaskCustomPatterns,
	}
}
