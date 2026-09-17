package history

import (
	"hanxi/internal/settings"
)

// HistoryService 统一历史记录的 Wails 绑定壳：薄转发 Store 的读删清三操作。
// Save 刻意不暴露——历史记录由后端业务动作自动产生，前端无写入面。
// 另挂 Q1 裁定的"OCR 全文入库"档位读写（持久化在 config.json，经 settings.Store）。
// 注册方式对齐 notify：公共包型服务直挂 services 列表，无模块身份、无 Nav、无路由。
type HistoryService struct {
	store    *Store
	settings *settings.Store
}

// NewHistoryService 装配历史服务；cfg 为全局配置存储（档位开关持久化用）。
func NewHistoryService(store *Store, cfg *settings.Store) *HistoryService {
	return &HistoryService{store: store, settings: cfg}
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

// GetOcrFullText 返回"OCR 识别全文入历史"开关（Q1：默认开=全文入库）。
func (h *HistoryService) GetOcrFullText() (bool, error) {
	if h.settings == nil {
		return true, nil
	}
	return h.settings.Get().HistoryOcrFullText, nil
}

// SetOcrFullText 设定"OCR 识别全文入历史"开关并落盘（对下一次识别起效）。
func (h *HistoryService) SetOcrFullText(v bool) error {
	if h.settings == nil {
		return nil
	}
	return h.settings.Update(func(cfg *settings.AppSettings) {
		cfg.HistoryOcrFullText = v
	})
}
