package portscan

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
		hasErr   bool
	}{
		{"80,443", []int{80, 443}, false},
		{"8000-8003", []int{8000, 8001, 8002, 8003}, false},
		{"80, 8080, 9000-9002", []int{80, 8080, 9000, 9001, 9002}, false},
		{"80, 80, 443", []int{80, 443}, false}, // 去重
		{"abc", nil, true},
		{"-1", nil, true},
		{"70000", nil, true},
	}

	for _, tc := range tests {
		got, err := ParsePortRange(tc.input)
		if tc.hasErr {
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", tc.input, err)
		}
		if len(got) != len(tc.expected) {
			t.Fatalf("input %q: expected %v, got %v", tc.input, tc.expected, got)
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Fatalf("input %q: at index %d expected %d, got %d", tc.input, i, tc.expected[i], got[i])
			}
		}
	}
}

func TestScannerScanLocalPort(t *testing.T) {
	// 启动一个临时的 local TCP 监听端口用于测试
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on local port: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port

	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	summary, err := scanner.ExecuteScan(
		ctx,
		"test_1",
		"127.0.0.1",
		[]int{port, port + 1},
		"",
		500*time.Millisecond,
		10,
		0,
		false,
		nil,
	)

	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(summary.OpenPorts) != 1 {
		t.Fatalf("expected 1 open port, got %d", len(summary.OpenPorts))
	}
	if summary.OpenPorts[0].Port != port {
		t.Fatalf("expected open port %d, got %d", port, summary.OpenPorts[0].Port)
	}
}

// startListeners 批量启动 n 个 127.0.0.1 临时监听器，返回端口集与清理函数。
// 无须 Accept：backlog 内 TCP 建连即成功，probePort 判定为 open。
func startListeners(t *testing.T, n int) ([]int, func()) {
	t.Helper()
	var ports []int
	var ls []net.Listener
	cleanup := func() {
		for _, l := range ls {
			_ = l.Close()
		}
	}
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			cleanup()
			t.Skipf("cannot listen on local port: %v", err)
		}
		ls = append(ls, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, cleanup
}

// grabClosedPorts 借一次性监听器占用 n 个临时端口后立即释放：
// 127.0.0.1 上无监听器的端口 connect 秒回 RST，即计为 closed。
// （沿用既有测试对"port+1 必然关闭"的同款本机回环假设）
func grabClosedPorts(t *testing.T, n int) []int {
	t.Helper()
	ports, cleanup := startListeners(t, n)
	cleanup()
	return ports
}

// eventLog 线程安全收集进度事件（worker 心跳与开放派发并发回调，与真实使用面同形）
type eventLog struct {
	mu     sync.Mutex
	events []ScanProgress
}

func (l *eventLog) add(p ScanProgress) {
	l.mu.Lock()
	l.events = append(l.events, p)
	l.mu.Unlock()
}

func (l *eventLog) snapshot() []ScanProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ScanProgress, len(l.events))
	copy(out, l.events)
	return out
}

// TestScannerOpenPortEventsDeliveredWhenSparse 稀疏开放场景：
// 每个开放端口都必须作为独立 latestPort 事件送达事件流——这是被修复的
// 单闸门 CAS 丢发缺陷的直接回归点（旧实现并发竞争下会被静默吞掉）。
// 节拍构造：单 worker + 每端口 60ms 微延迟，命中间隔（>50ms 合并窗）
// 保证每个槽位都能在下一个端口到达前被派发。
func TestScannerOpenPortEventsDeliveredWhenSparse(t *testing.T) {
	opens, cleanup := startListeners(t, 3)
	defer cleanup()
	ports := append([]int{}, opens...)
	ports = append(ports, grabClosedPorts(t, 2)...)

	log := &eventLog{}
	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	summary, err := scanner.ExecuteScan(ctx, "sparse_1", "127.0.0.1", ports, "",
		300*time.Millisecond, 1, 60, false, log.add)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(summary.OpenPorts) != len(opens) {
		t.Fatalf("summary: expected %d open ports, got %d", len(opens), len(summary.OpenPorts))
	}

	announced := make(map[int]bool)
	for _, p := range log.snapshot() {
		if p.LatestPort == nil {
			continue
		}
		if p.LatestPort.Status != PortOpen {
			t.Errorf("event carried non-open latestPort: %+v", p.LatestPort)
		}
		announced[p.LatestPort.Port] = true
	}
	for _, op := range opens {
		if !announced[op] {
			t.Errorf("open port %d was never announced as an event (silent-drop regression)", op)
		}
	}
}

// TestScannerDenseOpenNoPortLost 密集开放场景（48 端口在数毫秒内几乎同时开放）：
// 事件流按合并窗限速、只播最新端口+精确计数（合批是设计语义），但任何开放端口
// 都不得丢失——事件流出现的端口必须真实且属于开放集，最终送达由
// "事件并集 ∪ 终态 summary == 全开放集"断言（summary 是密集突发下的兜底送达面）。
// 同时全程 48 worker 并发回调，-race 下兼作并发安全验证。
func TestScannerDenseNoPortLost(t *testing.T) {
	opens, cleanup := startListeners(t, 48)
	defer cleanup()
	closed := grabClosedPorts(t, 16)
	ports := append(append([]int{}, opens...), closed...)
	openSet := make(map[int]bool, len(opens))
	for _, p := range opens {
		openSet[p] = true
	}

	log := &eventLog{}
	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	summary, err := scanner.ExecuteScan(ctx, "dense_1", "127.0.0.1", ports, "",
		300*time.Millisecond, 48, 0, false, log.add)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	// summary 完备且无重复：每个开放端口最终送达
	if len(summary.OpenPorts) != len(opens) {
		t.Fatalf("summary: expected %d open ports, got %d", len(opens), len(summary.OpenPorts))
	}
	seen := make(map[int]bool, len(summary.OpenPorts))
	for _, r := range summary.OpenPorts {
		if !openSet[r.Port] {
			t.Errorf("summary contains non-listening port %d", r.Port)
		}
		if seen[r.Port] {
			t.Errorf("summary duplicated port %d", r.Port)
		}
		seen[r.Port] = true
	}

	events := log.snapshot()
	var portEvents int
	for _, p := range events {
		if p.LatestPort == nil {
			continue
		}
		portEvents++
		if !openSet[p.LatestPort.Port] {
			t.Errorf("event announced bogus open port %d", p.LatestPort.Port)
		}
		if p.IsFinished {
			t.Errorf("finished event must not carry latestPort")
		}
	}
	if portEvents == 0 {
		t.Errorf("dense scan announced zero open ports through the event stream (channel broken?)")
	}
	// 突发抑制：带端口增量事件量必须受合并窗节拍封顶（末次补发允许 +1）
	if limit := int(elapsed/openMergeWindow) + 2; portEvents > limit {
		t.Errorf("open-event flood: %d port events in %v exceeds merge-window budget %d", portEvents, elapsed, limit)
	}

	// 终态事件计数恒准
	var finished *ScanProgress
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].IsFinished {
			finished = &events[i]
			break
		}
	}
	if finished == nil {
		t.Fatalf("missing finished event")
	}
	if finished.FoundOpen != len(opens) {
		t.Errorf("finished FoundOpen=%d, want %d", finished.FoundOpen, len(opens))
	}
}

// TestScannerPureProgressHeartbeatStaysThrottled 纯进度（无开放端口）场景：
// 心跳仍按 progressEmitInterval 限流——分闸修复不得把进度刷新变成逐端口刷屏。
// 节拍构造：单 worker + 每端口 10ms 微延迟 × 120 端口 ≈ 1.3s。
func TestScannerPureProgressHeartbeatStaysThrottled(t *testing.T) {
	ports := grabClosedPorts(t, 120)

	log := &eventLog{}
	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	summary, err := scanner.ExecuteScan(ctx, "throttle_1", "127.0.0.1", ports, "",
		300*time.Millisecond, 1, 10, false, log.add)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(summary.OpenPorts) != 0 {
		t.Fatalf("expected all-closed scan, got %d open", len(summary.OpenPorts))
	}

	events := log.snapshot()
	var heartbeats int
	var lastScanned int
	for _, p := range events {
		if p.LatestPort != nil {
			t.Errorf("closed-only scan emitted a port event: %+v", p.LatestPort)
		}
		if p.IsFinished {
			continue
		}
		heartbeats++
		if p.Scanned < lastScanned {
			t.Errorf("single-worker progress went backwards: %d after %d", p.Scanned, lastScanned)
		}
		lastScanned = p.Scanned
	}
	// 上界：≤100ms 一刷（+4 容差：首尾相位与终态前强制一刷）
	if limit := int(elapsed/progressEmitInterval) + 4; heartbeats > limit {
		t.Errorf("heartbeat flood: %d progress events in %v exceeds throttle budget %d", heartbeats, elapsed, limit)
	}
	// 下界：确实在持续推送而非只发终态
	if heartbeats < 3 {
		t.Errorf("heartbeat starvation: only %d progress events in %v", heartbeats, elapsed)
	}
}

// TestScannerCancelJoinsDispatcher ctx 取消路径：ExecuteScan 必须自行收口
// 开放派发协程后才返回（内部以 <-dispatchDone join，返回即证明无泄露协程）。
func TestScannerCancelJoinsDispatcher(t *testing.T) {
	opens, cleanup := startListeners(t, 4)
	defer cleanup()
	ports := append(append([]int{}, opens...), grabClosedPorts(t, 200)...)

	log := &eventLog{}
	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := scanner.ExecuteScan(ctx, "cancel_1", "127.0.0.1", ports, "",
			300*time.Millisecond, 16, 0, false, log.add)
		if err != nil {
			t.Errorf("cancelled scan returned error: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("ExecuteScan stuck after cancel (dispatcher not joined)")
	}
}
