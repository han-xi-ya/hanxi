package notify_test

import (
	"fmt"
	"sync"
	"testing"

	"hubkit/internal/notify"
)

func TestNotifyHub(t *testing.T) {
	hub := notify.GetHub()
	hub.ClearHistory()

	if len(hub.GetHistory()) != 0 {
		t.Fatalf("expected 0 history items, got %d", len(hub.GetHistory()))
	}

	// 1. 发送测试通知
	notify.Info("test", "测试标题", "测试内容", "/test")
	hist := hub.GetHistory()
	if len(hist) != 1 {
		t.Fatalf("expected 1 item, got %d", len(hist))
	}
	if hist[0].Title != "测试标题" || hist[0].Level != notify.LevelInfo {
		t.Errorf("unexpected item: %+v", hist[0])
	}

	// 2. 测试并发安全性与 100 条截断
	var wg sync.WaitGroup
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			notify.Success("test", fmt.Sprintf("标题 %d", idx), "并发内容", "/test")
		}(i)
	}
	wg.Wait()

	hist = hub.GetHistory()
	if len(hist) > 100 {
		t.Fatalf("history length exceeds 100: %d", len(hist))
	}

	// 3. 测试已读标记
	firstID := hist[0].ID
	hub.MarkAsRead(firstID)
	found := false
	for _, item := range hub.GetHistory() {
		if item.ID == firstID {
			found = true
			if !item.Read {
				t.Errorf("item %s should be read", firstID)
			}
		}
	}
	if !found {
		t.Errorf("item %s not found in history", firstID)
	}

	// 4. 全部已读
	hub.MarkAllAsRead()
	for _, item := range hub.GetHistory() {
		if !item.Read {
			t.Errorf("item %s should be read", item.ID)
		}
	}

	// 5. 清空
	hub.ClearHistory()
	if len(hub.GetHistory()) != 0 {
		t.Fatalf("expected 0 items after clear, got %d", len(hub.GetHistory()))
	}
}
