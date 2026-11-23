package history

// HistoryService 统一历史记录的 Wails 绑定壳：薄转发 Store 的读删清三操作。
// Save 刻意不暴露——历史记录由后端业务动作自动产生，前端无写入面。
// 注册方式对齐 notify：公共包型服务直挂 services 列表，无模块身份、无 Nav、无路由。
type HistoryService struct {
	store *Store
}

// NewHistoryService 装配历史服务。
func NewHistoryService(store *Store) *HistoryService {
	return &HistoryService{store: store}
}

// List 返回指定桶记录（新→旧）；keyword 非空时四字段大小写不敏感过滤。
func (h *HistoryService) List(funcType, keyword string) ([]Record, error) {
	return h.store.List(funcType, keyword)
}

// Delete 按全局 ID 删除单条记录。
func (h *HistoryService) Delete(id int64) error {
	return h.store.Delete(id)
}

// Clear 清空指定 funcType 桶（仅当前桶，不提供跨桶全清）。
func (h *HistoryService) Clear(funcType string) error {
	return h.store.Clear(funcType)
}
