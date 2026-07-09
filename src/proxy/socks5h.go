package proxy

import (
	"fmt"
	"net"
)

const socks5hRoutePrefix = "socks5h:"

func (r *Router) Socks5HEnabled() bool {
	return r != nil && r.socks5h.Enabled
}

func (r *Router) DialSocks5H(target string, mgr *UpstreamManager) (net.Conn, error) {
	if r == nil || !r.socks5h.Enabled {
		return nil, fmt.Errorf("socks5h redirect is disabled")
	}
	key := socks5hRoutePrefix + routeCacheKey(target)
	if entry, ok := r.cache.Get(key); ok && entry.Action == ActionProxy && entry.UpstreamName != "" {
		if up := mgr.Socks5ByName(entry.UpstreamName); up != nil {
			conn, err := up.DialSOCKS5H(target)
			if err == nil {
				return conn, nil
			}
			r.cache.Delete(key)
			mgr.markDead(up)
		}
	}

	var lastErr error
	names := r.socks5h.Upstreams
	for i := 0; i < mgr.Socks5AttemptCount(names); i++ {
		up := mgr.SelectSocks5For(names)
		if up == nil {
			break
		}
		conn, err := up.DialSOCKS5H(target)
		if err == nil {
			r.cache.SetProxy(key, up.Name, r.auto.CacheTTL)
			return conn, nil
		}
		lastErr = err
		mgr.markDead(up)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream")
	}
	return nil, lastErr
}
