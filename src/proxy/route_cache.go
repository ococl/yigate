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
}

func NewRouteCache() *RouteCache {
	return NewRouteCacheWithCap(defaultRouteCacheCap)
}

func NewRouteCacheWithCap(cap int) *RouteCache {
	if cap <= 0 {
		cap = defaultRouteCacheCap
	}
	return &RouteCache{entries: make(map[string]RouteCacheEntry), cap: cap}
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
