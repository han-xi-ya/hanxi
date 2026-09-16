// Package remoteversion 提供开发工具官网版本查询共享的缓存与安全 HTTP 能力。
package remoteversion

import (
	"fmt"
	"sync"
	"time"
)

// CacheTTL 官网数据新鲜期；超时后首次 Get 触发重取，重取失败则回吐旧数据（stale=true）。
const CacheTTL = 10 * time.Minute

// Cache 对单个官网版本源提供 TTL、并发合并和 stale-if-error 缓存。
// clone 必须做深拷贝：缓存内部数据与返回值共享引用会让调用方误改缓存。
// mu 全程持有（含 fetch），并发请求合并为一次网络往返，代价是慢源会阻塞同类型所有查询。
type Cache[T any] struct {
	mu        sync.Mutex
	data      T
	hasData   bool
	fetchedAt time.Time
	fetch     func() (T, error)
	clone     func(T) T
	now       func() time.Time
}

// NewCache 创建缓存；fetch 为实际网络取数函数，clone 提供 T 的深拷贝。now 固定 time.Now（单测可覆盖字段）。
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
