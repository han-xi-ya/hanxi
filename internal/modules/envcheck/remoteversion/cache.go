// Package remoteversion 提供开发工具官网版本查询共享的缓存与安全 HTTP 能力。
package remoteversion

import (
	"fmt"
	"sync"
	"time"
)

const CacheTTL = 10 * time.Minute

// Cache 对单个官网版本源提供 TTL、并发合并和 stale-if-error 缓存。
type Cache[T any] struct {
	mu        sync.Mutex
	data      T
	hasData   bool
	fetchedAt time.Time
	fetch     func() (T, error)
	clone     func(T) T
	now       func() time.Time
}

func NewCache[T any](fetch func() (T, error), clone func(T) T) *Cache[T] {
	return &Cache[T]{fetch: fetch, clone: clone, now: time.Now}
}

// Get 返回数据、是否为陈旧缓存及缓存实际获取时间。
func (c *Cache[T]) Get() (data T, stale bool, fetchedAt time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hasData && c.now().Sub(c.fetchedAt) < CacheTTL {
		return c.clone(c.data), false, c.fetchedAt, nil
	}
	fresh, fetchErr := c.fetch()
	if fetchErr == nil {
		c.data = c.clone(fresh)
		c.hasData = true
		c.fetchedAt = c.now()
		return c.clone(c.data), false, c.fetchedAt, nil
	}
	if c.hasData {
		return c.clone(c.data), true, c.fetchedAt, nil
	}
	var zero T
	return zero, false, time.Time{}, fmt.Errorf("官网版本查询失败: %w", fetchErr)
}
