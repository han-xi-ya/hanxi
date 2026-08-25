package notify

// Level 定义通知的严重级别
type Level string

const (
	LevelInfo    Level = "info"
	LevelSuccess Level = "success"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Notification 表示一条系统/业务通知
type Notification struct {
	ID        string `json:"id"`        // 唯一 ID
	ModuleID  string `json:"moduleId"`  // 来源模块 (如 "fileshare", "frpc", "system")
	Title     string `json:"title"`     // 简短标题
	Message   string `json:"message"`   // 正文内容
	Level     Level  `json:"level"`     // 严重级别
	Route     string `json:"route"`     // 点击跳转的前端路由 (如 "/ext/fileshare")
	Timestamp int64  `json:"timestamp"` // 发生时间 (Unix 毫秒)
	Read      bool   `json:"read"`      // 是否已读
}
