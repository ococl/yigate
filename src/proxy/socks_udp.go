package proxy

import (
	"bufio"
	"net"
	"sync"
	"time"
)

// maxUDPSessions 限制单个 UDP 关联里并存的目标会话数。
// 每个会话独占一个 socket 和一个 goroutine，不设上限时多目标 UDP 流量
// 可以在 90s 空闲回收之前耗尽 fd。
const maxUDPSessions = 128

// udpSession 表示一个 UDP 目标会话。
type udpSession struct {
	key        string
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
	r.stopOnce.Do(func() {
		close(r.quit)
		// 立刻唤醒阻塞在 ReadFromUDP 上的主循环。只关 quit 的话，中继退出
		// 最坏要等满一个 30s 读超时，会拖累连接的结束与进程优雅关闭。
		if r.ln != nil {
			_ = r.ln.Close()
		}
	})
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

// touchSession 刷新会话的活跃时间。必须按 key 直接定位：
// 遍历整个 map 会让每个回程包都付出 O(n) 代价，会话多时是自我放大。
// 会话已被淘汰或替换时不再更新。
func (r *udpRelay) touchSession(sess *udpSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.sessions[sess.key]; ok && current == sess {
		sess.lastUsed = time.Now()
	}
}

// sessionActive 判断会话是否仍在管理之中（未被淘汰）。
func (r *udpRelay) sessionActive(sess *udpSession) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[sess.key]
	return ok && current == sess
}

// evictOldestSessionLocked 淘汰最久未使用的会话，为建立新会话腾出位置。
// 调用方必须持有 r.mu。
func (r *udpRelay) evictOldestSessionLocked() {
	var oldestKey string
	var oldest *udpSession
	for key, sess := range r.sessions {
		if oldest == nil || sess.lastUsed.Before(oldest.lastUsed) {
			oldestKey, oldest = key, sess
		}
	}
	if oldest == nil {
		return
	}
	_ = oldest.conn.Close()
	delete(r.sessions, oldestKey)
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
		if len(r.sessions) >= maxUDPSessions {
			r.evictOldestSessionLocked()
		}
		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			r.mu.Unlock()
			return err
		}
		sess = &udpSession{key: target, targetAddr: udpAddr, conn: conn, lastUsed: time.Now()}
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
				// 会话被上限淘汰或被回收时 socket 已关闭，属于正常退出，
				// 不按故障告警。
				if !r.sessionActive(sess) {
					return
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
