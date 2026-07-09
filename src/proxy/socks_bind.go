package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// handleSOCKS5Bind 实现 SOCKS5 BIND 命令。
// 优先尝试通过 SOCKS5 上游级联；失败时自动回退到本地监听。
func handleSOCKS5Bind(client net.Conn, target string, mgr *UpstreamManager, router *Router) {
	if mgr != nil && mgr.Socks5AttemptCount(nil) > 0 {
		upConn, listenAddr, _, err := mgr.DialSOCKS5Bind(target, nil)
		if err == nil {
			defer upConn.Close()
			if err := relaySOCKS5Bind(client, upConn, listenAddr); err == nil {
				return
			}
			Logger().Warnw("socks5 bind upstream relay failed", "error", err)
		} else {
			Logger().Warnw("socks5 bind upstream failed", "target", target, "error", err)
		}
	}
	// 回退：本地 BIND
	localSOCKS5Bind(client, target)
}

// relaySOCKS5Bind 将上游 SOCKS5 BIND 的两次回复中继给客户端，并转发后续数据。
func relaySOCKS5Bind(client, upConn net.Conn, listenAddr string) error {
	if !socks5ReplyWithAddr(client, 0x00, listenAddr) {
		return errors.New("failed to send first bind reply to client")
	}

	// 读取上游第二次回复（包含目标地址）并转发给客户端
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

	Tunnel(client, nil, upConn)
	return nil
}

// localSOCKS5Bind 本地实现 SOCKS5 BIND。
func localSOCKS5Bind(client net.Conn, target string) {
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

	Tunnel(client, nil, incoming)
}

// handleSOCKS5UDPAssociate 实现 SOCKS5 UDP ASSOCIATE 命令。
// 优先尝试通过 SOCKS5 上游级联；失败时自动回退到本地 UDP 中继。
func handleSOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, target string, mgr *UpstreamManager, router *Router) {
	if mgr != nil && mgr.Socks5AttemptCount(nil) > 0 {
		upConn, upUDPAddr, _, err := mgr.DialSOCKS5UDP(target, nil)
		if err == nil {
			if err := relaySOCKS5UDPAssociate(client, clientReader, upConn, upUDPAddr); err == nil {
				return
			}
			Logger().Warnw("socks5 udp associate upstream relay failed", "error", err)
			upConn.Close()
		} else {
			Logger().Warnw("socks5 udp associate upstream failed", "target", target, "error", err)
		}
	}
	// 回退：本地 UDP 中继
	localSOCKS5UDPAssociate(client, clientReader, target)
}

// relaySOCKS5UDPAssociate 通过上游 SOCKS5 UDP ASSOCIATE 做 UDP 中继。
func relaySOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, upConn net.Conn, upUDPAddr *net.UDPAddr) error {
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
		sessions:     make(map[string]*udpSession),
		quit:         make(chan struct{}),
	}
	relay.run(clientReader)
	return nil
}

// localSOCKS5UDPAssociate 本地实现 SOCKS5 UDP ASSOCIATE。
func localSOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, target string) {
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
		sessions:    make(map[string]*udpSession),
		quit:        make(chan struct{}),
	}
	relay.run(clientReader)
}

func parsePort(s string) (int, bool) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 0 || p > 65535 {
		return 0, false
	}
	return p, true
}

// readSOCKS5UDPHeader 解析 SOCKS5 UDP 请求头，返回目标地址、分片号和负载起始偏移。
func readSOCKS5UDPHeader(buf []byte) (addr string, frag byte, dataOff int, ok bool) {
	if len(buf) < 10 {
		return "", 0, 0, false
	}
	if buf[0] != 0x00 || buf[1] != 0x00 {
		return "", 0, 0, false
	}
	frag = buf[2]
	atyp := buf[3]
	dataOff = 4

	var host string
	switch atyp {
	case 0x01:
		if len(buf) < dataOff+4+2 {
			return "", 0, 0, false
		}
		host = net.IP(buf[dataOff : dataOff+4]).String()
		dataOff += 4
	case 0x03:
		if len(buf) < dataOff+1 {
			return "", 0, 0, false
		}
		l := int(buf[dataOff])
		dataOff++
		if len(buf) < dataOff+l+2 {
			return "", 0, 0, false
		}
		host = string(buf[dataOff : dataOff+l])
		dataOff += l
	case 0x04:
		if len(buf) < dataOff+16+2 {
			return "", 0, 0, false
		}
		host = net.IP(buf[dataOff : dataOff+16]).String()
		dataOff += 16
	default:
		return "", 0, 0, false
	}
	if len(buf) < dataOff+2 {
		return "", 0, 0, false
	}
	port := int(buf[dataOff])<<8 | int(buf[dataOff+1])
	dataOff += 2
	return net.JoinHostPort(host, strconv.Itoa(port)), frag, dataOff, true
}

// buildSOCKS5UDPHeader 构造 SOCKS5 UDP 回复头。
func buildSOCKS5UDPHeader(target string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	var header []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			header = []byte{0x00, 0x00, 0x00, 0x01}
			header = append(header, ip4...)
		} else {
			header = []byte{0x00, 0x00, 0x00, 0x04}
			header = append(header, ip.To16()...)
		}
	} else {
		header = []byte{0x00, 0x00, 0x00, 0x03, byte(len(host))}
		header = append(header, []byte(host)...)
	}
	header = append(header, byte(port>>8), byte(port))
	return header, nil
}

// socks5ReplyPacket 表示 SOCKS5 回复包。
type socks5ReplyPacket struct {
	rep byte
	raw []byte
}

// readSOCKS5Reply 从连接读取完整的 SOCKS5 回复。
func readSOCKS5Reply(r io.Reader) (*socks5ReplyPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if header[0] != 0x05 {
		return nil, errors.New("invalid socks5 reply version")
	}

	var addrLen int
	switch header[3] {
	case 0x01:
		addrLen = 4
	case 0x03:
		size := make([]byte, 1)
		if _, err := r.Read(size); err != nil {
			return nil, err
		}
		addrLen = int(size[0])
		header = append(header, size...)
	case 0x04:
		addrLen = 16
	default:
		return nil, fmt.Errorf("unsupported reply address type %d", header[3])
	}
	addr := make([]byte, addrLen+2)
	if _, err := r.Read(addr); err != nil {
		return nil, err
	}
	return &socks5ReplyPacket{rep: header[1], raw: append(header, addr...)}, nil
}

// udpSession 表示一个 UDP 目标会话。
type udpSession struct {
	targetAddr *net.UDPAddr
	conn       *net.UDPConn
	lastUsed   time.Time
}

// udpRelay 管理一个 UDP ASSOCIATE 会话。
type udpRelay struct {
	clientConn    net.Conn
	clientUDPAddr *net.UDPAddr
	ln            *net.UDPConn
	upConn        net.Conn     // 上游 TCP 控制连接（级联模式）
	upPacketConn  *net.UDPConn // 上游 UDP 连接（级联模式）
	useUpstream   bool
	sessions      map[string]*udpSession
	mu            sync.Mutex
	quit          chan struct{}
}

func (r *udpRelay) run(clientReader *bufio.Reader) {
	if r.upConn != nil {
		// 级联模式：上游 TCP 控制连接一旦关闭，UDP 中继也应终止。
		go func() {
			buf := make([]byte, 256)
			for {
				n, err := r.upConn.Read(buf)
				if n == 0 || err != nil {
					close(r.quit)
					return
				}
			}
		}()
	}
	go func() {
		// 客户端 TCP 控制连接一旦关闭，UDP 中继也应终止。
		buf := make([]byte, 256)
		for {
			n, err := r.clientConn.Read(buf)
			if n == 0 || err != nil {
				close(r.quit)
				return
			}
		}
	}()

	go r.gcSessions()

	// 级联模式：同时监听上游 UDP 回复
	if r.useUpstream && r.upPacketConn != nil {
		go r.readFromUpstream()
	}

	buf := make([]byte, 65535)
	for {
		if err := r.ln.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			Logger().Warnw("udp relay set read deadline failed", "error", err)
			r.closeSessions()
			return
		}
		n, srcAddr, err := r.ln.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-r.quit:
				r.closeSessions()
				return
			default:
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				Logger().Warnw("udp relay read failed", "error", err)
				r.closeSessions()
				return
			}
		}

		if r.clientUDPAddr == nil {
			r.clientUDPAddr = srcAddr
		} else if r.clientUDPAddr.IP.String() != srcAddr.IP.String() {
			Logger().Warnw("udp relay ignore packet", "from", srcAddr, "expected", r.clientUDPAddr)
			continue
		}

		target, frag, dataOff, ok := readSOCKS5UDPHeader(buf[:n])
		if !ok {
			Logger().Warnw("udp relay invalid header", "from", srcAddr)
			continue
		}
		if frag != 0 {
			Logger().Warnw("udp relay fragment not supported", "frag", frag)
			continue
		}
		data := buf[dataOff:n]

		if r.useUpstream && r.upPacketConn != nil {
			packet := append(buf[:dataOff], data...)
			if _, err := r.upPacketConn.Write(packet); err != nil {
				Logger().Warnw("udp relay write to upstream failed", "error", err)
			}
			continue
		}

		if err := r.forwardToTarget(target, data); err != nil {
			Logger().Warnw("udp relay forward to target failed", "target", target, "error", err)
		}
	}
}

func (r *udpRelay) forwardToTarget(target string, data []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return err
	}

	r.mu.Lock()
	sess, ok := r.sessions[target]
	if !ok {
		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			r.mu.Unlock()
			return err
		}
		sess = &udpSession{targetAddr: udpAddr, conn: conn, lastUsed: time.Now()}
		r.sessions[target] = sess
		go r.readFromTarget(sess)
	}
	sess.lastUsed = time.Now()
	conn := sess.conn
	r.mu.Unlock()

	_, err = conn.Write(data)
	return err
}

func (r *udpRelay) readFromTarget(sess *udpSession) {
	buf := make([]byte, 65535)
	header, err := buildSOCKS5UDPHeader(sess.targetAddr.String())
	if err != nil {
		Logger().Warnw("udp relay build header failed", "error", err)
		return
	}
	for {
		if err := sess.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			Logger().Warnw("udp session set read deadline failed", "error", err)
			return
		}
		n, err := sess.conn.Read(buf)
		if err != nil {
			select {
			case <-r.quit:
				return
			default:
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				Logger().Warnw("udp session read failed", "error", err)
				return
			}
		}
		if r.clientUDPAddr == nil {
			continue
		}
		packet := append(header, buf[:n]...)
		if _, err := r.ln.WriteToUDP(packet, r.clientUDPAddr); err != nil {
			Logger().Warnw("udp relay write back to client failed", "error", err)
			return
		}
		sess.lastUsed = time.Now()
	}
}

func (r *udpRelay) readFromUpstream() {
	buf := make([]byte, 65535)
	for {
		if err := r.upPacketConn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			Logger().Warnw("udp upstream set read deadline failed", "error", err)
			return
		}
		n, err := r.upPacketConn.Read(buf)
		if err != nil {
			select {
			case <-r.quit:
				return
			default:
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				Logger().Warnw("udp upstream read failed", "error", err)
				return
			}
		}
		if r.clientUDPAddr == nil {
			continue
		}
		if _, err := r.ln.WriteToUDP(buf[:n], r.clientUDPAddr); err != nil {
			Logger().Warnw("udp relay write back to client failed", "error", err)
			return
		}
	}
}

func (r *udpRelay) gcSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.quit:
			return
		case <-ticker.C:
			r.mu.Lock()
			for key, sess := range r.sessions {
				if time.Since(sess.lastUsed) > 90*time.Second {
					sess.conn.Close()
					delete(r.sessions, key)
				}
			}
			r.mu.Unlock()
		}
	}
}

func (r *udpRelay) closeSessions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sess := range r.sessions {
		sess.conn.Close()
	}
	clear(r.sessions)
}
