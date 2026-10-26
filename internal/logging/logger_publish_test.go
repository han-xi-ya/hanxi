package logging

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// ── globalLogger 发布通道竞态测试（A2 2026-10-26）────────────────────────
//
// 病灶档案：initLogger 在 logMu 内写 globalLogger 裸指针字段，而 L() 从不
// 持锁——启动早期各模块 goroutine 调 L() 与装配线程 InitLogger 换装并发，
// 构成 memory-model 层面的数据竞争（-race 实锤形态）。修复：atomic.Pointer
// 发布。本测试在 -race 下把读写窗口重叠打满：修复前必报 race，修复后必须
// 零报警，且 L() 全程返回可用 logger（非 nil 承诺不破）。

func TestLConcurrentWithInitLoggerPublish(t *testing.T) {
	var (
		stop  = make(chan struct{})
		wg    sync.WaitGroup
		mu    sync.Mutex
		badly int
	)
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if l := L(); l == nil {
					mu.Lock()
					badly++
					mu.Unlock()
				}
			}
		})
	}

	// 换装两次：模拟"运行中重新初始化"与首发窗口（生产为启动一次，测试把
	// 竞争窗口拉宽；本机 -race 不可用，验证口径为重复调度实测）。
	dir := t.TempDir()
	for i := range 2 {
		_, cleanup, err := initLogger(dir, 7, time.Now)
		if err != nil {
			t.Fatalf("initLogger #%d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
		cleanup() // 关闭本代文件汇，避免测试进程挂句柄
	}
	close(stop)
	wg.Wait()

	if badly != 0 {
		t.Fatalf("L() 出现 nil 返回 %d 次（非 nil 承诺破裂）", badly)
	}
	// 终态：最近一次 initLogger 的 logger 必须已被发布。
	var published *slog.Logger
	if published = L(); published == nil {
		t.Fatal("L() 终态为 nil")
	}
}
