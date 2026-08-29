package app

import (
	"fmt"
	"log/slog"
	"time"

	"hanxi/internal/notify"
)

// SendTestNotification 发送一条测试通知 (用于在设置页面测试前后台通知效果)
func (s *AppService) SendTestNotification() {
	slog.Info("triggering SendTestNotification from frontend")
	notify.Success("system", "Hanxi 统一通知", "这是一条即时测试通知：窗口激活时为应用内卡片！", "/settings")
}

// SendDelayedTestNotification 延迟指定秒数后发送通知（专用于测试最小化/后台时的 Windows 原生桌面通知气泡）
func (s *AppService) SendDelayedTestNotification(delaySeconds int) {
	if delaySeconds <= 0 {
		delaySeconds = 3
	}
	slog.Info("scheduling delayed notification", "delay", delaySeconds)
	go func() {
		time.Sleep(time.Duration(delaySeconds) * time.Second)
		notify.Success("system", "Hanxi 后台桌面通知", fmt.Sprintf("这是 %d 秒前触发的延迟通知：成功在后台/最小化时弹出 Windows 原生桌面气泡！", delaySeconds), "/settings")
	}()
}
