package notify

// Send 发送一条通知（核心入口）
func Send(n *Notification) {
	GetHub().Emit(n)
}

// Info 发送一条信息级别通知
func Info(moduleID, title, message, route string) {
	Send(&Notification{
		ModuleID: moduleID,
		Title:    title,
		Message:  message,
		Level:    LevelInfo,
		Route:    route,
	})
}

// Success 发送一条成功级别通知
func Success(moduleID, title, message, route string) {
	Send(&Notification{
		ModuleID: moduleID,
		Title:    title,
		Message:  message,
		Level:    LevelSuccess,
		Route:    route,
	})
}

// Warning 发送一条警告级别通知
func Warning(moduleID, title, message, route string) {
	Send(&Notification{
		ModuleID: moduleID,
		Title:    title,
		Message:  message,
		Level:    LevelWarning,
		Route:    route,
	})
}

// Error 发送一条错误级别通知
func Error(moduleID, title, message, route string) {
	Send(&Notification{
		ModuleID: moduleID,
		Title:    title,
		Message:  message,
		Level:    LevelError,
		Route:    route,
	})
}
