package proxy

import (
	"bufio"
	"net"
)

// HandleSOCKS5 处理 SOCKS5 代理请求，并在 socks5h_redirect 开启时尝试恢复域名
func HandleSOCKS5(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router, access *AccessController, allowBind, allowUDP bool) {
	defer client.Close()
	MetricInc("yigate_requests_total", "protocol=socks5")

	user, err := socks5NegotiateAuth(client, clientReader, users)
	if err != nil {
		Logger().Warnw("socks5 auth failed", "error", err)
		return
	}
	if access != nil && user != "" {
		if !access.AcquireUserSlot(user) {
			Logger().Warnw("socks5 user connection limit exceeded", "user", user)
			socks5Reply(client, 0x02)
			return
		}
		defer access.ReleaseUserSlot(user)
	}

	cmd, target, rep, err := socks5ReadRequest(clientReader)
	if err != nil {
		Logger().Warnw("socks5 request failed", "error", err)
		if rep == 0 {
			rep = 0x01
		}
		socks5Reply(client, rep)
		return
	}

	switch cmd {
	case 0x01: // CONNECT
		handleSOCKS5Connect(client, clientReader, target, mgr, router, access, user)
	case 0x02: // BIND
		if !allowBind {
			Logger().Warnw("socks5 bind command disabled", "target", target)
			socks5Reply(client, 0x07)
			return
		}
		handleSOCKS5Bind(client, target, mgr, router, user)
	case 0x03: // UDP ASSOCIATE
		if !allowUDP {
			Logger().Warnw("socks5 udp associate command disabled", "target", target)
			socks5Reply(client, 0x07)
			return
		}
		handleSOCKS5UDPAssociate(client, clientReader, target, mgr, router, user)
	default:
		Logger().Warnw("socks5 unsupported command", "command", cmd)
		socks5Reply(client, 0x07)
	}
}

func handleSOCKS5Connect(client net.Conn, clientReader *bufio.Reader, target string, mgr *UpstreamManager, router *Router, access *AccessController, user string) {
	sourceIP := clientSourceIP(client)
	host, _, err := net.SplitHostPort(target)
	if router != nil && router.Socks5HEnabled() && err == nil && net.ParseIP(host) != nil {
		Logger().Infow("socks5 ip target will be handled with sniff/rewrite", "target", target)
		handleSOCKS5ConnectWithSniff(client, target, mgr, router, access, user, sourceIP)
		return
	}

	targetConn, err := dialTargetForRequest(target, mgr, router, sourceIP)
	if err != nil {
		Logger().Warnw("socks5 connect failed", "target", target, "error", err)
		socks5Reply(client, SOCKS5ReplyFromError(err))
		return
	}
	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set socks5 client deadline failed", "error", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set socks5 target deadline failed", "error", err)
		targetConn.Close()
		return
	}

	socks5Reply(client, 0x00)
	Tunnel(client, clientReader, targetConn, newTrafficKey(user, targetConn, target), getGlobalTrafficAccounter())
}
