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
		return RouteCacheEntry{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return RouteCacheEntry{}, false
	}
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		c.Delete(key)
		return RouteCacheEntry{}, false
	}
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
