package notify_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"hanxi/internal/notify"
)

// ── Hub.GetHistory 快照语义测试（A2 2026-10-26）───────────────────────────
//
// 病灶档案：GetHistory 曾返回共享指针切片——调用方（Wails 绑定把返回值交给
// JSON 序列化器，运行在锁外）逐字段读取的同时，MarkAsRead/MarkAllAsRead 可
// 持写锁就地改同一批对象：锁外读 vs 锁内写构成数据竞争（-race 实锤）；
// 且调用方误改返回项会反向污染 Hub 状态。修复后 GetHistory 逐项拷贝。

// TestGetHistoryReturnsDetachedSnapshot 外部可观察事实：对返回项的一切改写
// （含模拟调用方"手滑写脏数据"）都不得回流到 Hub 账上。
func TestGetHistoryReturnsDetachedSnapshot(t *testing.T) {
	hub := notify.GetHub()
	hub.ClearHistory()
	t.Cleanup(hub.ClearHistory)

	notify.Info("a2-probe", "原始标题", "原始正文", "/probe")
	id := hub.GetHistory()[0].ID

	snap := hub.GetHistory()
	snap[0].Title = "调用方误改"
	snap[0].Read = true
	snap[0].Message = "污染尝试"

	again := hub.GetHistory()
	if again[0].Title != "原始标题" || again[0].Message != "原始正文" {
		t.Fatalf("返回项被误写后污染了 Hub 内存态: %+v", again[0])
	}
	if again[0].Read {
		t.Fatal("未经 MarkAsRead 的快照改写不得回流账本")
	}

	// 正向语义仍在：显式 MarkAsRead 走 Hub 通道生效并可被后续快照看到。
	hub.MarkAsRead(id)
	if !hub.GetHistory()[0].Read {
		t.Fatal("MarkAsRead 经快照通道后必须仍然生效")
	}
}

// TestHubConcurrentReadMarkMarshalStress 复现病灶触发形态：一批 goroutine
// 取历史并 JSON 序列化（模拟绑定路径在锁外读字段），另一批并发标已读。
// 修复前该形态是教科书式 data race（共享 *Notification 字段锁外读 vs 锁内写），
// CI -race 档下预期先红；本机无 gcc 未跑 -race，以重复调度（-count=5）实测
// 收敛为准，race 判定本身"仅推断未本机实测"。
func TestHubConcurrentReadMarkMarshalStress(t *testing.T) {
	hub := notify.GetHub()
	hub.ClearHistory()
	t.Cleanup(hub.ClearHistory)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			notify.Info("a2-race", "并发标题", "并发正文", "/race")
		})
	}
	for range 6 {
		wg.Go(func() {
			for range 50 {
				list := hub.GetHistory()
				if _, err := json.Marshal(list); err != nil {
					t.Errorf("快照序列化失败: %v", err)
					return
				}
			}
		})
	}
	for range 3 {
		wg.Go(func() {
			for range 50 {
				hub.MarkAllAsRead()
				hub.MarkAsRead("不存在的ID")
			}
		})
	}
	wg.Wait()

	// 终态断言必须在**静默之后**取：上面 20 个 Info 与 3 个 MarkAllAsRead 循环并发，
	// 一条 Info 完全可能在最后一次 mark-all 之后才落地——那"并发落定即全部已读"根本
	// 不是可成立的命题（本用例此前偶发红就是栽在这）。压力照旧并发跑，收敛判定改为
	// "静默 → 一次 mark-all → 全部已读"，这才是产品语义（用户点「全部已读」后的结果）。
	hub.MarkAllAsRead()

	final := hub.GetHistory()
	if len(final) > 100 {
		t.Fatalf("历史超上限: %d", len(final))
	}
	if len(final) == 0 {
		t.Fatal("并发写入后历史为空（写入根本没落到 hub）")
	}
	for _, n := range final {
		if !n.Read {
			t.Fatalf("静默后 MarkAllAsRead 仍有未读项残留: id=%s title=%s", n.ID, n.Title)
		}
		if !strings.HasPrefix(n.Title, "并发") {
			t.Fatalf("快照内容异常: %+v", n)
		}
	}
}
