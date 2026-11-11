package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"
)

// handleSOCKS5Bind 实现 SOCKS5 BIND 命令。
// 优先尝试通过 SOCKS5 上游级联；失败时自动回退到本地监听。

// bindSecondReplyTimeout 是等待上游 BIND 第二次回复的最长时间，
// 与本地 BIND 等待对端连入的 Accept 超时一致。测试可调小。
var bindSecondReplyTimeout = 30 * time.Second

func handleSOCKS5Bind(client net.Conn, target string, mgr *UpstreamManager, router *Router, user string) {
	if router != nil && router.Policy() != nil {
		if err := router.Policy().CheckTarget(target); err != nil {
			Logger().Warnw("socks5 bind target denied by policy", "target", target, "error", err)
			socks5Reply(client, 0x02)
			return
		}
	}
	if mgr != nil && mgr.Socks5AttemptCount(nil) > 0 {
		upConn, listenAddr, _, err := mgr.DialSOCKS5Bind(target, nil)
		if err == nil {
			defer upConn.Close()
			if err := relaySOCKS5Bind(client, upConn, listenAddr, user); err == nil {
				return
			}
			Logger().Warnw("socks5 bind upstream relay failed", "error", err)
		} else {
			Logger().Warnw("socks5 bind upstream failed", "target", target, "error", err)
		}
	}
	// 回退：本地 BIND
	localSOCKS5Bind(client, target, router.Policy(), user)
}

// relaySOCKS5Bind 将上游 SOCKS5 BIND 的两次回复中继给客户端，并转发后续数据。
func relaySOCKS5Bind(client, upConn net.Conn, listenAddr string, user string) error {
	if !socks5ReplyWithAddr(client, 0x00, listenAddr) {
		return errors.New("failed to send first bind reply to client")
	}

	// 读取上游第二次回复（包含目标地址）并转发给客户端。
	// 上游握手成功后会清除连接 deadline，而第二次回复取决于对端何时连入，
	// 不设超时就会让 goroutine 与客户端连接永久挂起。与本地 BIND 等待
	// Accept 的 30s 保持一致。
	if err := upConn.SetReadDeadline(time.Now().Add(bindSecondReplyTimeout)); err != nil {
		return fmt.Errorf("set upstream bind read deadline: %w", err)
	}
	reply, err := readSOCKS5Reply(upConn)
	if err != nil {
		return fmt.Errorf("read second bind reply from upstream: %w", err)
	}
	if reply.rep != 0x00 {
		socks5Reply(client, reply.rep)
		return fmt.Errorf("upstream second bind reply: 0x%02x", reply.rep)
	}
	if _, err := client.Write(reply.raw); err != nil {
		return fmt.Errorf("write second bind reply to client: %w", err)
	}

	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		return fmt.Errorf("set client deadline: %w", err)
	}
	if err := setConnDeadline(upConn, TunnelIdleTimeout()); err != nil {
		return fmt.Errorf("set upstream deadline: %w", err)
	}

	Tunnel(client, nil, upConn, TrafficKey{User: user, Upstream: upstreamNameOf(upConn)}, getGlobalTrafficAccounter())
	return nil
}

// localSOCKS5Bind 本地实现 SOCKS5 BIND。
func localSOCKS5Bind(client net.Conn, target string, policy *DestinationPolicy, user string) {
	if policy != nil {
		if err := policy.CheckTarget(target); err != nil {
			Logger().Warnw("socks5 bind target denied by policy", "target", target, "error", err)
			socks5Reply(client, 0x02)
			return
		}
	}
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		Logger().Warnw("socks5 bind listen failed", "error", err)
		socks5Reply(client, 0x01)
		return
	}
	defer ln.Close()

	if !socks5ReplyWithAddr(client, 0x00, ln.Addr().String()) {
		return
	}

	expectedHost, _, _ := net.SplitHostPort(target)
	if err := ln.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		Logger().Warnw("socks5 bind set deadline failed", "error", err)
		socks5Reply(client, 0x01)
		return
	}

	incoming, err := ln.Accept()
	if err != nil {
		Logger().Warnw("socks5 bind accept failed", "error", err)
		socks5Reply(client, 0x01)
		return
	}
	defer incoming.Close()

	if expectedHost != "" {
		if host, _, _ := net.SplitHostPort(incoming.RemoteAddr().String()); host != expectedHost {
			Logger().Warnw("socks5 bind connection rejected", "from", host, "expected", expectedHost)
			socks5Reply(client, 0x02)
			return
		}
	}

	if !socks5ReplyWithAddr(client, 0x00, incoming.RemoteAddr().String()) {
		return
	}

	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set socks5 bind client deadline failed", "error", err)
		return
	}
	if err := setConnDeadline(incoming, TunnelIdleTimeout()); err != nil {
		Logger().Warnw("set socks5 bind incoming deadline failed", "error", err)
		return
	}

	Tunnel(client, nil, incoming, newTrafficKey(user, incoming, target), getGlobalTrafficAccounter())
}

// handleSOCKS5UDPAssociate 实现 SOCKS5 UDP ASSOCIATE 命令。
// 优先尝试通过 SOCKS5 上游级联；失败时自动回退到本地 UDP 中继。
func handleSOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, target string, mgr *UpstreamManager, router *Router, user string) {
	// UDP 关联的寿命不应由 TCP 控制连接的握手 deadline 决定：控制连接只用于
	// 探测客户端是否离开。入口在握手前对整条连接设了 deadline，若不清除，
	// 握手超时一到，仍在正常转发数据的 UDP 中继会被误判为客户端断开。
	if err := setConnDeadline(client, 0); err != nil {
		Logger().Warnw("clear socks5 udp control deadline failed", "error", err)
	}
	policy := router.Policy()
	if mgr != nil && mgr.Socks5AttemptCount(nil) > 0 {
		upConn, upUDPAddr, _, err := mgr.DialSOCKS5UDP(target, nil)
		if err == nil {
			if err := relaySOCKS5UDPAssociate(client, clientReader, upConn, upUDPAddr, policy); err == nil {
				return
			}
			Logger().Warnw("socks5 udp associate upstream relay failed", "error", err)
			upConn.Close()
		} else {
			Logger().Warnw("socks5 udp associate upstream failed", "target", target, "error", err)
		}
	}
	// 回退：本地 UDP 中继
	localSOCKS5UDPAssociate(client, clientReader, target, policy)
}

// relaySOCKS5UDPAssociate 通过上游 SOCKS5 UDP ASSOCIATE 做 UDP 中继。
func relaySOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, upConn net.Conn, upUDPAddr *net.UDPAddr, policy *DestinationPolicy) error {
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	ln, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	defer ln.Close()

	if !socks5ReplyWithAddr(client, 0x00, ln.LocalAddr().String()) {
		return errors.New("failed to send udp associate reply to client")
	}

	// 建立到上游 UDP 端口的连接
	upPacketConn, err := net.DialUDP("udp", nil, upUDPAddr)
	if err != nil {
		return fmt.Errorf("dial upstream udp: %w", err)
	}
	defer upPacketConn.Close()

	relay := &udpRelay{
		clientConn:   client,
		ln:           ln,
		upConn:       upConn,
		upPacketConn: upPacketConn,
		useUpstream:  true,
		policy:       policy,
		sessions:     make(map[string]*udpSession),
		quit:         make(chan struct{}),
	}
	relay.run(clientReader)
	return nil
}

// localSOCKS5UDPAssociate 本地实现 SOCKS5 UDP ASSOCIATE。
func localSOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, target string, policy *DestinationPolicy) {
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	if host, port, _ := net.SplitHostPort(target); host != "" {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			addr.IP = ip
		}
		if p, _ := parsePort(port); p > 0 {
			addr.Port = p
		}
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		Logger().Warnw("socks5 udp associate listen failed", "error", err)
		socks5Reply(client, 0x01)
		return
	}
	defer conn.Close()

	if !socks5ReplyWithAddr(client, 0x00, conn.LocalAddr().String()) {
		return
	}

	relay := &udpRelay{
		clientConn:  client,
		ln:          conn,
		useUpstream: false,
		policy:      policy,
		sessions:    make(map[string]*udpSession),
		quit:        make(chan struct{}),
	}
	relay.run(clientReader)
}
