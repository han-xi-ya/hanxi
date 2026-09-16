// Package notify 提供全局通知中心：业务模块经 Send/Info 等入口投递通知，
// Hub 负责历史缓存、事件广播到前端通知中心，并在窗口后台时补发 Windows 原生 Toast。
// 依赖方向：仅依赖 Wails application 事件系统，任何模块可安全引用。
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
