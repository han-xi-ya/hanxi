package memo

import "time"

// MemoItem 便签核心数据模型
type MemoItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`     // 标签数组 (如 ["#SQL", "#Token", "#Todo"])
	IsPinned  bool      `json:"isPinned"` // 是否置顶
	IsMasked  bool      `json:"isMasked"` // 是否敏感脱敏遮罩 (如 API Key / Token)
	ColorTag  string    `json:"colorTag"` // 侧边色彩标识 (如 "blue", "green", "amber", "purple", "rose")
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MemoFilter 检索与过滤条件
type MemoFilter struct {
	Keyword  string `json:"keyword"`  // 搜索关键字 (分词模糊匹配 Title, Content, Tags)
	Tag      string `json:"tag"`      // 精确标签过滤
	Pinned   *bool  `json:"pinned"`   // 是否仅展示置顶
	SortBy   string `json:"sortBy"`   // "updated" | "created"
	SortDesc bool   `json:"sortDesc"` // 是否降序 (默认 true)
}

// MemoStats 便签统计与标签云数据
type MemoStats struct {
	TotalCount  int            `json:"totalCount"`
	PinnedCount int            `json:"pinnedCount"`
	TagCloud    map[string]int `json:"tagCloud"` // 每个标签对应的便签计数
}
