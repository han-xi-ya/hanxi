package notify

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// historyCap 通知历史环形保留上限：仅驻留最近 N 条，防止长期运行内存无界增长。
const historyCap = 100

// Hub 是全局统一通知中心分发器
type Hub struct {
	mu      sync.RWMutex
	history []*Notification
	app     *application.App
	win     *application.WebviewWindow
	seq     atomic.Uint64
}

var globalHub = &Hub{
	history: make([]*Notification, 0, historyCap),
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
	// 头插法保留最新通知，上限 historyCap 条
	h.history = append([]*Notification{n}, h.history...)
	if len(h.history) > historyCap {
		h.history = h.history[:historyCap]
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

// GetHistory 获取历史通知列表。
//
// 审查修复（A2 2026-10-26）：曾返回共享指针切片——调用方（Wails 绑定 JSON
// 序列化）释放读锁后才逐字段读取，而并发的 MarkAsRead/MarkAllAsRead 持写锁
// 就地改同一批对象，锁外读取与锁内写入构成数据竞争（-race 可实锤）；调用方
// 误改返回项还会反向污染 Hub 状态。改为逐项快照拷贝：返回态是读锁窗口内的
// 一致视图，后续并发改判与调用方误写都不再触及返回数据。
func (h *Hub) GetHistory() []*Notification {
	h.mu.RLock()
	defer h.mu.RUnlock()
	res := make([]*Notification, len(h.history))
	for i, n := range h.history {
		cp := *n
		res[i] = &cp
	}
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
	h.history = make([]*Notification, 0, historyCap)
}
