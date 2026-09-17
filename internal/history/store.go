package history

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hanxi/internal/jsonstore"
	"hanxi/internal/logging"
)

const (
	// FileName 状态文件名（落 <StateDir>/history.json，随数据根治理布局）。
	FileName = "history.json"

	// maxPerBucket 每桶保留上限（编译期常量，对齐需求"约 200"与 MooTool 口径）。
	maxPerBucket = 200
	// maxFieldRunes Input/Output 截断上限（rune 计，中文安全）。
	maxFieldRunes = 4000
	// truncatedSuffix 截断标记。
	truncatedSuffix = "…[truncated]"
	// summaryFromInputRunes Summary 留空时取 Input 前 N 字自动生成。
	summaryFromInputRunes = 40
)

// Store 历史记录存储：内存权威副本 + 每次变更全量原子覆写落盘。
// 所有公开方法整体持锁（单用户桌面写入频率远低于全量写成本，一把锁收口最简）；
// 损坏（loadErr 驻留）后禁一切写，读侧返回错误供面板提示。
type Store struct {
	mu      sync.Mutex
	path    string
	buckets map[string][]Record
	loadErr error
	lastID  int64 // 已分配/已见最大 ID，新 ID 严格大于它（重启续接）
}

// NewStore 以 stateDir 为落点创建存储并立即加载存量。
// 文件不存在视为空库首次运行；损坏/空文件置 loadErr 驻留（严格档，禁写防覆写）。
func NewStore(stateDir string) *Store {
	s := &Store{
		path:    filepath.Join(stateDir, FileName),
		buckets: map[string][]Record{},
	}
	var buckets map[string][]Record
	ok, err := jsonstore.Load(s.path, &buckets)
	switch {
	case err != nil:
		// ErrEmpty / ErrCorrupt / IO 错误：不采信可能部分填充的值（jsonstore 契约），
		// 内存保持空库但驻留 loadErr 禁止落盘，存量字节原位保留待人工处置。
		s.loadErr = err
		slog.Error("history: 存量文件不可用，历史记录降级为只读禁写（需人工删除该文件才能恢复写入）",
			"path", s.path, "err", err)
	case ok:
		if buckets != nil {
			s.buckets = buckets
		}
	}
	for _, b := range s.buckets {
		for _, r := range b {
			if r.ID > s.lastID {
				s.lastID = r.ID
			}
		}
	}
	return s
}

// Save 补 ID/时间、统一脱敏与截断后头插进对应桶并裁剪落盘。
// 落盘失败返回错误但内存不回滚（历史是尽力而为的副作用，不拖累业务）。
func (s *Store) Save(r Record) error {
	if strings.TrimSpace(r.FuncType) == "" {
		return fmt.Errorf("history: funcType 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}

	r = s.normalizeLocked(r)
	s.buckets[r.FuncType] = append([]Record{r}, s.buckets[r.FuncType]...)
	if len(s.buckets[r.FuncType]) > maxPerBucket {
		s.buckets[r.FuncType] = s.buckets[r.FuncType][:maxPerBucket]
	}
	return s.saveLocked()
}

// List 返回某桶记录（新→旧）。keyword 非空时对 Summary/Input/Output/Extra
// 做大小写不敏感的 Contains 过滤。loadErr 驻留时返回错误供面板提示。
func (s *Store) List(funcType, keyword string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	src := s.buckets[funcType]
	out := make([]Record, 0, len(src))
	kw := strings.ToLower(strings.TrimSpace(keyword))
	for _, r := range src {
		if kw != "" && !matchKeyword(r, kw) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Delete 按全局 ID 跨桶删除单条记录并落盘；ID 不存在返回错误。
func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	for ft, b := range s.buckets {
		for i, r := range b {
			if r.ID != id {
				continue
			}
			s.buckets[ft] = append(b[:i:i], b[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("history: 未找到 ID 为 %d 的记录", id)
}

// Clear 清空指定 funcType 桶（Q5 裁定：仅作用当前桶，不提供跨桶全清）。
// 桶本不存在时按无操作成功处理，不产生写盘。
func (s *Store) Clear(funcType string) error {
	if strings.TrimSpace(funcType) == "" {
		return fmt.Errorf("history: funcType 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	if _, ok := s.buckets[funcType]; !ok {
		return nil
	}
	delete(s.buckets, funcType)
	return s.saveLocked()
}

// Degraded 返回损坏驻留错误（nil = 正常）。服务侧可选用于更醒目的面板提示。
func (s *Store) Degraded() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// normalizeLocked 补全 ID/时间、统一脱敏与截断（调用方必须已持锁）。
// 先脱敏后截断：截断可能切断凭据正则的匹配段，顺序不可颠倒。
func (s *Store) normalizeLocked(r Record) Record {
	if r.ID == 0 {
		r.ID = s.nextIDLocked()
	} else if r.ID > s.lastID {
		s.lastID = r.ID
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().Format(time.RFC3339)
	}
	r.Summary = truncate(logging.Redact(r.Summary))
	r.Input = truncate(logging.Redact(r.Input))
	r.Output = truncate(logging.Redact(r.Output))
	r.Extra = truncate(logging.Redact(r.Extra))
	if strings.TrimSpace(r.Summary) == "" {
		r.Summary = headRunes(r.Input, summaryFromInputRunes)
	}
	return r
}

// nextIDLocked 时间戳毫秒*1000 起步、与已见最大值严格递增（时钟回拨不产生乱序 ID）。
func (s *Store) nextIDLocked() int64 {
	cand := time.Now().UnixMilli() * 1000
	if cand <= s.lastID {
		cand = s.lastID + 1
	}
	s.lastID = cand
	return cand
}

// saveLocked 全量原子覆写（调用方必须已持锁）。失败记日志并透传，内存不动。
func (s *Store) saveLocked() error {
	if err := jsonstore.Save(s.path, s.buckets); err != nil {
		slog.Error("history: 落盘失败（内存态保留，下次操作可再试）", "path", s.path, "err", err)
		return err
	}
	return nil
}

// matchKeyword 四字段大小写不敏感包含匹配（kw 须已小写）。
func matchKeyword(r Record, kw string) bool {
	return strings.Contains(strings.ToLower(r.Summary), kw) ||
		strings.Contains(strings.ToLower(r.Input), kw) ||
		strings.Contains(strings.ToLower(r.Output), kw) ||
		strings.Contains(strings.ToLower(r.Extra), kw)
}

// truncate rune 计截断，超限截到上限并追加标记。
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxFieldRunes {
		return s
	}
	return headRunes(s, maxFieldRunes) + truncatedSuffix
}

func headRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
