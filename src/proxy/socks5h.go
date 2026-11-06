package proxy

import (
	"fmt"
	"net"
)

const socks5hRoutePrefix = "socks5h:"

func (r *Router) Socks5HEnabled() bool {
	return r != nil && r.socks5h.Enabled
}

func (r *Router) DialSocks5H(target string, mgr *UpstreamManager, sourceIP ...string) (net.Conn, error) {
	if r == nil || !r.socks5h.Enabled {
		return nil, fmt.Errorf("socks5h redirect is disabled")
	}
	key := socks5hRoutePrefix + routeCacheKey(target)
	if entry, ok := r.cache.Get(key); ok && entry.Action == ActionProxy && entry.UpstreamName != "" {
		if up := mgr.Socks5ByName(entry.UpstreamName); up != nil {
			conn, err := up.DialSOCKS5H(target)
			if err == nil {
				mgr.markAlive(up)
				return tagUpstream(conn, up.Name), nil
			}
			r.cache.Delete(key)
			if IsUpstreamError(err) {
				mgr.markDead(up)
			}
		}
	}

	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	var lastErr error
	names := r.socks5h.Upstreams
	tried := make(map[*Upstream]struct{})
	for i := 0; i < mgr.Socks5AttemptCount(names); i++ {
		var up *Upstream
		if sip != "" {
			up = mgr.SelectSocks5ForWithSourceIP(names, sip)
		} else {
			up = mgr.SelectSocks5For(names)
		}
		if up == nil {
			break
		}
		if _, dup := tried[up]; dup {
			continue
		}
		tried[up] = struct{}{}
		conn, err := up.DialSOCKS5H(target)
		if err == nil {
			mgr.markAlive(up)
			r.cache.SetProxy(key, up.Name, r.auto.CacheTTL)
			return tagUpstream(conn, up.Name), nil
		}
		lastErr = err
		if IsUpstreamError(err) {
			mgr.markDead(up)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available socks5 upstream")
	}
	return nil, lastErr
}
