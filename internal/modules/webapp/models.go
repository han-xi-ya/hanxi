package webapp

// WebAppEntryView 前端列表行的条目模型（settings.WebAppEntry + 运行时窗态）。
// 窗态由服务层窗口表推导：WindowOpen=窗口存在且可见，
// WindowHidden=窗口收起驻留中（隐藏 + TTL 待销毁）。
type WebAppEntryView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	Icon         string `json:"icon"`
	CreatedAt    string `json:"createdAt"`
	WindowOpen   bool   `json:"windowOpen"`
	WindowHidden bool   `json:"windowHidden"`
}
