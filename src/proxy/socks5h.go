package proxy

import (
	"fmt"
	"net"
	"time"
)

const socks5hRoutePrefix = "socks5h:"

// socks5h 熔断冷却时间：DialSocks5H 失败后在此期间直接跳过尝试，
// 避免每个 HTTP 请求都经历一次完整的 EOF 等待。
const socks5hBreakerCooldown = 10 * time.Second

func (r *Router) Socks5HEnabled() bool {
	return r != nil && r.socks5h.Enabled
}

// socks5hBreakUntil 存储熔断到期时间（unix nano）。
// 0 表示未熔断。
func (r *Router) socks5hBreakUntil() int64 {
	if r == nil {
		return 0
	}
	return r.socks5hBreaker.Load()
}

// tripSocks5HBreaker 设置熔断到期时间。
func (r *Router) tripSocks5HBreaker() {
	if r == nil {
		return
	}
	r.socks5hBreaker.Store(time.Now().Add(socks5hBreakerCooldown).UnixNano())
}

// socks5hAvailable 检查熔断是否已过期。
func (r *Router) socks5hAvailable() bool {
	until := r.socks5hBreakUntil()
	if until == 0 {
		return true
	}
	return time.Now().UnixNano() > until
}

func (r *Router) DialSocks5H(target string, mgr *UpstreamManager, sourceIP ...string) (net.Conn, error) {
	if r == nil || !r.socks5h.Enabled {
		return nil, fmt.Errorf("socks5h redirect is disabled")
	}
	// 熔断期间直接返回错误，跳过无效尝试。
	if !r.socks5hAvailable() {
		return nil, fmt.Errorf("socks5h redirect temporarily unavailable (circuit breaker open)")
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

	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	var lastErr error
	names := r.socks5h.Upstreams
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
	// 无可用上游时触发熔断，避免后续请求重复尝试。
	r.tripSocks5HBreaker()
	return nil, lastErr
}
