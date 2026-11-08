package proxy

import (
	"bufio"
	"net"
	"sync"
	"time"
)

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
	policy        *DestinationPolicy
	mu            sync.Mutex
	clientMu      sync.RWMutex
	stopOnce      sync.Once
	quit          chan struct{}
}

func (r *udpRelay) signalStop() {
	r.stopOnce.Do(func() { close(r.quit) })
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	return &net.UDPAddr{IP: append(net.IP(nil), addr.IP...), Port: addr.Port, Zone: addr.Zone}
}

func (r *udpRelay) acceptClientAddr(src *net.UDPAddr) bool {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if r.clientUDPAddr == nil {
		r.clientUDPAddr = cloneUDPAddr(src)
		return true
	}
	return r.clientUDPAddr.IP.Equal(src.IP) && r.clientUDPAddr.Port == src.Port && r.clientUDPAddr.Zone == src.Zone
}

func (r *udpRelay) clientAddr() *net.UDPAddr {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return cloneUDPAddr(r.clientUDPAddr)
}

func (r *udpRelay) touchSession(sess *udpSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.sessions {
		if current == sess {
			sess.lastUsed = time.Now()
			return
		}
	}
}

func (r *udpRelay) run(clientReader *bufio.Reader) {
	defer r.closeSessions()
	if r.upConn != nil {
		// 级联模式：上游 TCP 控制连接一旦关闭，UDP 中继也应终止。
		go func() {
			buf := make([]byte, 256)
			for {
				n, err := r.upConn.Read(buf)
				if n == 0 || err != nil {
					r.signalStop()
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
				r.signalStop()
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

		if !r.acceptClientAddr(srcAddr) {
			Logger().Warnw("udp relay ignore packet", "from", srcAddr, "expected", r.clientAddr())
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
			// 级联模式由上游解析并转发，同样不能绕过目标访问策略。
			if r.policy != nil {
				if err := r.policy.CheckTarget(target); err != nil {
					Logger().Warnw("udp relay upstream target denied by policy", "target", target, "error", err)
					continue
				}
			}
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
	if r.policy != nil {
		if err := r.policy.Check(udpAddr.IP.String(), udpAddr.Port, udpAddr.IP); err != nil {
			Logger().Warnw("udp relay target denied by policy", "target", target, "error", err)
			return err
		}
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
		clientAddr := r.clientAddr()
		if clientAddr == nil {
			continue
		}
		packet := append(header, buf[:n]...)
		if _, err := r.ln.WriteToUDP(packet, clientAddr); err != nil {
			Logger().Warnw("udp relay write back to client failed", "error", err)
			return
		}
		r.touchSession(sess)
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
		clientAddr := r.clientAddr()
		if clientAddr == nil {
			continue
		}
		if _, err := r.ln.WriteToUDP(buf[:n], clientAddr); err != nil {
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
