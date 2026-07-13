package proxy

import (
	"net"
)

// TProxyConfig 透明代理配置。
// 支持两种模式：
//   - redirect: 使用 iptables REDIRECT + SO_ORIGINAL_DST 获取原始目标（简单，不保留源 IP）
//   - tproxy:   使用 iptables TPROXY + IP_TRANSPARENT，通过 conn.LocalAddr() 获取原始目标（保留源 IP）
type TProxyConfig struct {
	Enabled bool   `yaml:"enabled"`
	Mode    string `yaml:"mode"` // "redirect"（默认）或 "tproxy"
}

// IsTProxyEnabled 返回透明代理是否启用。
func (c TProxyConfig) IsTProxyEnabled() bool {
	return c.Enabled
}

// NormalizedMode 返回规范化后的模式字符串。
func (c TProxyConfig) NormalizedMode() string {
	if c.Mode == "tproxy" {
		return "tproxy"
	}
	return "redirect"
}

// HandleTProxy 处理透明代理连接。
// 根据模式从连接中获取原始目标地址，然后通过路由器建立到目标的连接并双向转发。
func HandleTProxy(client net.Conn, mgr *UpstreamManager, router *Router, mode string) {
	defer client.Close()
	MetricInc("k5pw_requests_total", "protocol=tproxy")

	target, err := getOriginalDestination(client, mode)
	if err != nil {
		Logger().Warnw("tproxy get original destination failed", "mode", mode, "error", err)
		return
	}

	sourceIP := clientSourceIP(client)
	targetConn, err := dialTargetForRequest(target, mgr, router, sourceIP)
	if err != nil {
		Logger().Warnw("tproxy connect failed", "target", target, "error", err)
		return
	}

	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set tproxy client deadline failed", "error", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set tproxy target deadline failed", "error", err)
		targetConn.Close()
		return
	}

	Logger().Infow("tproxy tunnel established", "target", target, "source", sourceIP)
	Tunnel(client, nil, targetConn, TrafficKey{Target: targetHostFromAddr(target)}, globalTrafficAccounter)
}

// getOriginalDestination 根据模式获取原始目标地址。
func getOriginalDestination(conn net.Conn, mode string) (string, error) {
	if mode == "tproxy" {
		// TPROXY 模式：连接的 LocalAddr 就是原始目标地址
		addr := conn.LocalAddr()
		if addr == nil {
			return "", errNoOriginalDst
		}
		return addr.String(), nil
	}
	// REDIRECT 模式：通过 SO_ORIGINAL_DST 获取
	return originalDst(conn)
}
