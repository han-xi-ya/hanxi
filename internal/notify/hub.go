package notify

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Hub 是全局统一通知中心分发器
type Hub struct {
	mu      sync.RWMutex
	history []*Notification
	app     *application.App
	win     *application.WebviewWindow
	seq     atomic.Uint64
}

var globalHub = &Hub{
	history: make([]*Notification, 0, 100),
}

// GetHub 获取全局通知分发器单例
func GetHub() *Hub {
	return globalHub
}

// SetWailsContext 注入 Wails App 与主窗口引用
func (h *Hub) SetWailsContext(app *application.App, win *application.WebviewWindow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.app = app
	h.win = win
}

// Emit 发送一条通知
func (h *Hub) Emit(n *Notification) {
	if n == nil {
		return
	}
	if n.ID == "" {
		n.ID = fmt.Sprintf("notif_%d_%d", time.Now().UnixMilli(), h.seq.Add(1))
	}
	if n.Timestamp == 0 {
		n.Timestamp = time.Now().UnixMilli()
	}
	if n.Level == "" {
		n.Level = LevelInfo
	}

	h.mu.Lock()
	// 头插法保留最新通知，上限 100 条
	h.history = append([]*Notification{n}, h.history...)
	if len(h.history) > 100 {
		h.history = h.history[:100]
	}
	appRef := h.app
	winRef := h.win
	h.mu.Unlock()

	// 1. 广播 Wails 事件给前端 (通知前端组件和未读红点)
	// 注意: RegisterEvent[notify.Notification] 注册的是值类型，Emit 必须传值
	// (传 *Notification 指针会因注册类型严格校验失败而被 Wails 取消丢弃)
	if appRef != nil && appRef.Event != nil {
		appRef.Event.Emit("notify:received", *n)
	} else if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("notify:received", *n)
	}

	// 2. 检查窗口是否在后台/最小化，若在后台则触发 Windows 原生系统 Toast
	isBg := winRef != nil && (!winRef.IsVisible() || winRef.IsMinimised())
	if isBg {
		showNativeToast(n)
	}
}

// GetHistory 获取历史通知列表
func (h *Hub) GetHistory() []*Notification {
	h.mu.RLock()
	defer h.mu.RUnlock()
	res := make([]*Notification, len(h.history))
	copy(res, h.history)
	return res
}

// MarkAsRead 将指定通知标记为已读
func (h *Hub) MarkAsRead(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, item := range h.history {
		if item.ID == id {
			item.Read = true
			break
		}
	}
}

// MarkAllAsRead 将所有通知标记为已读
func (h *Hub) MarkAllAsRead() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, item := range h.history {
		item.Read = true
	}
}

// ClearHistory 清空历史通知
func (h *Hub) ClearHistory() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = make([]*Notification, 0, 100)
}
