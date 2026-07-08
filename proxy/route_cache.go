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

type RouteCache struct {
	mu      sync.RWMutex
	entries map[string]RouteCacheEntry
}

func NewRouteCache() *RouteCache {
	return &RouteCache{entries: make(map[string]RouteCacheEntry)}
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
	c.mu.Unlock()
}

func (c *RouteCache) set(key string, entry RouteCacheEntry) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	c.entries[key] = entry
	c.mu.Unlock()
}

func expiresAt(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}
