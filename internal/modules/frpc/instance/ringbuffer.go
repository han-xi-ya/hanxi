package instance

import (
	"strings"
	"sync"
)

// RingBuffer 线程安全环形日志缓冲：固定容量，写满后覆盖最旧记录，保留最近 N 行。
type RingBuffer struct {
	mu    sync.Mutex
	lines []string // 环形复用
	head  int      // 下一个写入位置
	count int      // 当前有效记录数
	cap   int
}

// newRingBuffer 创建容量为 cap 的环形缓冲。
func newRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 1
	}
	return &RingBuffer{
		lines: make([]string, cap),
		cap:   cap,
	}
}

// Write 写入一行（空行忽略）。
func (rb *RingBuffer) Write(line string) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.lines[rb.head] = line
	rb.head = (rb.head + 1) % rb.cap
	if rb.count < rb.cap {
		rb.count++
	}
}

// Last 返回最近 n 行（按时间从旧到新）。
// n <= 0 或 n >= 容量时返回全部有效记录。
func (rb *RingBuffer) Last(n int) []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if n <= 0 || n > rb.count {
		n = rb.count
	}
	out := make([]string, 0, n)
	// 从最近 n 条的最旧位置开始：head-n 起，n 个
	start := (rb.head - n + rb.cap) % rb.cap
	for i := 0; i < n; i++ {
		out = append(out, rb.lines[(start+i)%rb.cap])
	}
	return out
}

// Len 返回当前有效记录数。
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}