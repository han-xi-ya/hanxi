package memo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/settings"
)

// MemoService 便签业务服务
type MemoService struct {
	store    *Store
	mu       sync.RWMutex
	items    []MemoItem
	wailsApp *application.App
}

// NewMemoService 实例化便签服务
func NewMemoService(paths *settings.Paths) (*MemoService, error) {
	memoPath := filepath.Join(paths.DataDir(), "memo.json")
	store, err := NewStore(memoPath)
	if err != nil {
		return nil, err
	}

	items, err := store.Load()
	if err != nil {
		items = []MemoItem{}
	}

	return &MemoService{
		store: store,
		items: items,
	}, nil
}

// SetWailsApp 设置 Wails App 引用
func (s *MemoService) SetWailsApp(app *application.App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wailsApp = app
}

// List 根据过滤条件检索便签
func (s *MemoService) List(filter MemoFilter) []MemoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]MemoItem, 0, len(s.items))
	kw := strings.ToLower(strings.TrimSpace(filter.Keyword))
	filterTag := strings.TrimSpace(filter.Tag)

	for _, item := range s.items {
		// 1. 过滤置顶
		if filter.Pinned != nil && item.IsPinned != *filter.Pinned {
			continue
		}

		// 2. 过滤标签
		if filterTag != "" {
			matchedTag := false
			for _, t := range item.Tags {
				if strings.EqualFold(t, filterTag) || strings.EqualFold(strings.TrimPrefix(t, "#"), strings.TrimPrefix(filterTag, "#")) {
					matchedTag = true
					break
				}
			}
			if !matchedTag {
				continue
			}
		}

		// 3. 关键字模糊搜索 (标题、内容、标签)
		if kw != "" {
			titleMatch := strings.Contains(strings.ToLower(item.Title), kw)
			contentMatch := strings.Contains(strings.ToLower(item.Content), kw)
			tagMatch := false
			for _, t := range item.Tags {
				if strings.Contains(strings.ToLower(t), kw) {
					tagMatch = true
					break
				}
			}
			if !titleMatch && !contentMatch && !tagMatch {
				continue
			}
		}

		result = append(result, item)
	}

	// 排序逻辑：置顶始终排在最前面；其次按 UpdatedAt 或 CreatedAt 排序
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsPinned != result[j].IsPinned {
			return result[i].IsPinned // true 在前
		}
		if filter.SortBy == "created" {
			if filter.SortDesc {
				return result[i].CreatedAt.After(result[j].CreatedAt)
			}
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		// 默认按 updatedAt 降序
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})

	return result
}

// GetStats 获取便签统计数据与标签云
func (s *MemoService) GetStats() MemoStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := MemoStats{
		TotalCount:  len(s.items),
		PinnedCount: 0,
		TagCloud:    make(map[string]int),
	}

	for _, item := range s.items {
		if item.IsPinned {
			stats.PinnedCount++
		}
		for _, tag := range item.Tags {
			cleaned := strings.TrimSpace(tag)
			if cleaned != "" {
				if !strings.HasPrefix(cleaned, "#") {
					cleaned = "#" + cleaned
				}
				stats.TagCloud[cleaned]++
			}
		}
	}

	return stats
}

// Create 创建新便签
func (s *MemoService) Create(title, content string, tags []string, colorTag string) (MemoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cleanedTags := cleanTags(tags)
	if colorTag == "" {
		colorTag = "blue"
	}

	item := MemoItem{
		ID:        fmt.Sprintf("memo_%d", now.UnixNano()),
		Title:     strings.TrimSpace(title),
		Content:   content,
		Tags:      cleanedTags,
		IsPinned:  false,
		IsMasked:  false,
		ColorTag:  colorTag,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.items = append([]MemoItem{item}, s.items...)
	if err := s.store.Save(s.items); err != nil {
		return MemoItem{}, err
	}

	s.emitChanged()
	return item, nil
}

// QuickCreate 快捷创建 (主要供 fileshare 跨模块投递联动使用)
func (s *MemoService) QuickCreate(title, content string, tags []string) error {
	_, err := s.Create(title, content, tags, "amber")
	return err
}

// Update 更新已有便签
func (s *MemoService) Update(id, title, content string, tags []string, colorTag string) (MemoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, it := range s.items {
		if it.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return MemoItem{}, fmt.Errorf("便签不存在: %s", id)
	}

	s.items[idx].Title = strings.TrimSpace(title)
	s.items[idx].Content = content
	s.items[idx].Tags = cleanTags(tags)
	if colorTag != "" {
		s.items[idx].ColorTag = colorTag
	}
	s.items[idx].UpdatedAt = time.Now()

	updated := s.items[idx]
	if err := s.store.Save(s.items); err != nil {
		return MemoItem{}, err
	}

	s.emitChanged()
	return updated, nil
}

// TogglePin 切换置顶状态
func (s *MemoService) TogglePin(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, it := range s.items {
		if it.ID == id {
			s.items[i].IsPinned = !s.items[i].IsPinned
			s.items[i].UpdatedAt = time.Now()
			cur := s.items[i].IsPinned
			_ = s.store.Save(s.items)
			s.emitChanged()
			return cur, nil
		}
	}
	return false, fmt.Errorf("便签不存在: %s", id)
}

// ToggleMask 切换敏感信息遮罩
func (s *MemoService) ToggleMask(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, it := range s.items {
		if it.ID == id {
			s.items[i].IsMasked = !s.items[i].IsMasked
			cur := s.items[i].IsMasked
			_ = s.store.Save(s.items)
			s.emitChanged()
			return cur, nil
		}
	}
	return false, fmt.Errorf("便签不存在: %s", id)
}

// Delete 删除指定便签
func (s *MemoService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]MemoItem, 0, len(s.items))
	for _, it := range s.items {
		if it.ID != id {
			filtered = append(filtered, it)
		}
	}

	s.items = filtered
	if err := s.store.Save(s.items); err != nil {
		return err
	}

	s.emitChanged()
	return nil
}

func (s *MemoService) emitChanged() {
	if s.wailsApp != nil && s.wailsApp.Event != nil {
		s.wailsApp.Event.Emit("memo:changed", nil)
	} else if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("memo:changed", nil)
	}
}

func cleanTags(tags []string) []string {
	seen := make(map[string]bool)
	res := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			t = "#" + t
		}
		if !seen[t] {
			seen[t] = true
			res = append(res, t)
		}
	}
	return res
}
