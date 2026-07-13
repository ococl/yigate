package proxy

import (
	"sync"
	"time"
)

type RouteCacheEntry struct {
	Action       string
	UpstreamName string
	ExpiresAt    time.Time
}

const defaultRouteCacheCap = 10000

type RouteCache struct {
	mu      sync.RWMutex
	entries map[string]RouteCacheEntry
	order   []string
	cap     int

	stop chan struct{}
	wg   sync.WaitGroup
}

// defaultRouteCacheCleanupInterval 是后台过期清理的周期。
const defaultRouteCacheCleanupInterval = time.Minute

func NewRouteCache() *RouteCache {
	return NewRouteCacheWithCap(defaultRouteCacheCap)
}

func NewRouteCacheWithCap(cap int) *RouteCache {
	if cap <= 0 {
		cap = defaultRouteCacheCap
	}
	return &RouteCache{
		entries: make(map[string]RouteCacheEntry),
		cap:     cap,
		stop:    make(chan struct{}),
	}
}

// StartCleanup 启动后台过期清理 goroutine。
// 注意：构造函数不会自动启动，必须显式调用（由 Router.Start 在运行时调用），
// 这样测试直接构造 RouteCache / Router 时不会泄漏 goroutine。
func (c *RouteCache) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = defaultRouteCacheCleanupInterval
	}
	c.wg.Add(1)
	go c.cleanupLoop(interval)
}

// Stop 停止后台清理并等待退出，避免热加载后残留 goroutine。
func (c *RouteCache) Stop() {
	if c == nil {
		return
	}
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	c.wg.Wait()
}

func (c *RouteCache) cleanupLoop(interval time.Duration) {
	defer c.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.evictExpired()
		}
	}
}

// evictExpired 删除所有已过期的条目，回收长期驻留但不再被访问的内存。
func (c *RouteCache) evictExpired() {
	c.mu.Lock()
	now := time.Now()
	expired := make([]string, 0, len(c.entries))
	for k, e := range c.entries {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			expired = append(expired, k)
		}
	}
	for _, k := range expired {
		delete(c.entries, k)
		c.removeFromOrderLocked(k)
	}
	c.mu.Unlock()
}

func (c *RouteCache) Get(key string) (RouteCacheEntry, bool) {
	if c == nil || key == "" {
		MetricInc("k5pw_cache_misses_total")
		return RouteCacheEntry{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		MetricInc("k5pw_cache_misses_total")
		return RouteCacheEntry{}, false
	}
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		// 原子地删除过期条目：在写锁下重新校验 ExpiresAt，
		// 避免误删在 RUnlock 与 Lock 之间被其他 goroutine 写入的新值。
		c.deleteIfExpired(key, entry.ExpiresAt)
		MetricInc("k5pw_cache_misses_total")
		return RouteCacheEntry{}, false
	}
	MetricInc("k5pw_cache_hits_total")
	return entry, true
}

func (c *RouteCache) SetDirect(key string, ttl time.Duration) {
	c.set(key, RouteCacheEntry{Action: ActionDirect, ExpiresAt: expiresAt(ttl)})
}

func (c *RouteCache) SetProxy(key, upstreamName string, ttl time.Duration) {
	if upstreamName == "" {
		return
	}
	c.set(key, RouteCacheEntry{Action: ActionProxy, UpstreamName: upstreamName, ExpiresAt: expiresAt(ttl)})
}

func (c *RouteCache) Delete(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.removeFromOrderLocked(key)
	c.mu.Unlock()
}

// deleteIfExpired 在写锁下删除 key，仅当当前条目的 ExpiresAt 与 staleExpiresAt 一致时才删除。
// 这样即使另一个 goroutine 在读锁释放后写入了一个新的（未过期）条目，也不会被误删。
func (c *RouteCache) deleteIfExpired(key string, staleExpiresAt time.Time) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	cur, ok := c.entries[key]
	if !ok || !cur.ExpiresAt.Equal(staleExpiresAt) {
		// 条目已被更新或删除，不操作。
		c.mu.Unlock()
		return
	}
	delete(c.entries, key)
	c.removeFromOrderLocked(key)
	c.mu.Unlock()
}

func (c *RouteCache) set(key string, entry RouteCacheEntry) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		c.removeFromOrderLocked(key)
	}
	c.order = append(c.order, key)
	c.entries[key] = entry
	for len(c.entries) > c.cap && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

func (c *RouteCache) removeFromOrderLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func expiresAt(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}
