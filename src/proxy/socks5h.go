package proxy

import (
	"errors"
	"fmt"
	"net"
)

const socks5hRoutePrefix = "socks5h:"

// ErrSocks5HNoUpstream 表示当前没有任何启用的 SOCKS5 上游可用于重定向。
// 调用方据此把 socks5h_redirect 当作未开启处理，静默回退到普通路由。
var ErrSocks5HNoUpstream = errors.New("no socks5 upstream available for socks5h redirect")

// SetSocks5HAvailable 注入"当前是否存在可用 SOCKS5 上游"的判定。
// 未注入时按配置字面值生效（兼容不装配 UpstreamManager 的场景）。
func (r *Router) SetSocks5HAvailable(fn func(names []string) bool) {
	if r == nil {
		return
	}
	r.socks5hAvail = fn
}

// Socks5HEnabled 报告 socks5h 重定向是否应生效。
//
// 配置开启但没有任何可用 SOCKS5 上游时等价于未开启：直接走普通路由，
// 既不做无谓的拨号尝试，也不产生"socks5h redirect failed"的告警噪音。
func (r *Router) Socks5HEnabled() bool {
	if r == nil || !r.socks5h.Enabled {
		return false
	}
	if fn := r.socks5hAvail; fn != nil && !fn(r.socks5h.Upstreams) {
		return false
	}
	return true
}

func (r *Router) DialSocks5H(target string, mgr *UpstreamManager, sourceIP ...string) (net.Conn, error) {
	if r == nil || !r.socks5h.Enabled {
		return nil, fmt.Errorf("socks5h redirect is disabled")
	}
	// 没有任何可用的 SOCKS5 上游：返回可识别的哨兵错误，让调用方
	// 静默回退，而不是当成一次真实的重定向失败去告警。
	if mgr == nil || mgr.Socks5AttemptCount(r.socks5h.Upstreams) == 0 {
		return nil, ErrSocks5HNoUpstream
	}
	if err := r.CheckTarget(target); err != nil {
		return nil, err
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
			if IsNodeFailure(err) {
				mgr.markPassiveFailure(up)
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
		if IsNodeFailure(err) {
			mgr.markPassiveFailure(up)
		}
	}
	if lastErr == nil {
		lastErr = ErrSocks5HNoUpstream
	}
	return nil, lastErr
}
