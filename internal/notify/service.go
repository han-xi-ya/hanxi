package notify

// NotificationService 暴露给 Wails 前端的通知中心管理服务
type NotificationService struct{}

func NewNotificationService() *NotificationService {
	return &NotificationService{}
}

// GetHistory 获取历史通知
func (s *NotificationService) GetHistory() []*Notification {
	return GetHub().GetHistory()
}

// MarkAsRead 标记某条通知为已读
func (s *NotificationService) MarkAsRead(id string) {
	GetHub().MarkAsRead(id)
}

// MarkAllAsRead 全部标记为已读
func (s *NotificationService) MarkAllAsRead() {
	GetHub().MarkAllAsRead()
}

// ClearHistory 清空历史通知
func (s *NotificationService) ClearHistory() {
	GetHub().ClearHistory()
}
