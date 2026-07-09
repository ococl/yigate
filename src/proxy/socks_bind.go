package proxy

import (
	"bufio"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// handleSOCKS5Bind 实现 SOCKS5 BIND 命令。
// 客户端在请求中给出预期连接来源；服务器监听一个随机端口，等待目标连接，
// 建立后进入双向转发。
func handleSOCKS5Bind(client net.Conn, target string) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		log.Printf("socks5 bind listen: %v", err)
		socks5Reply(client, 0x01)
		return
	}
	defer ln.Close()

	if !socks5ReplyWithAddr(client, 0x00, ln.Addr().String()) {
		return
	}

	expectedHost, _, _ := net.SplitHostPort(target)
	if err := ln.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		log.Printf("socks5 bind set deadline: %v", err)
		socks5Reply(client, 0x01)
		return
	}

	incoming, err := ln.Accept()
	if err != nil {
		log.Printf("socks5 bind accept: %v", err)
		socks5Reply(client, 0x01)
		return
	}
	defer incoming.Close()

	if expectedHost != "" {
		if host, _, _ := net.SplitHostPort(incoming.RemoteAddr().String()); host != expectedHost {
			log.Printf("socks5 bind connection from %s rejected, expected %s", host, expectedHost)
			socks5Reply(client, 0x02)
			return
		}
	}

	if !socks5ReplyWithAddr(client, 0x00, incoming.RemoteAddr().String()) {
		return
	}

	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		log.Printf("set socks5 bind client deadline: %v", err)
		return
	}
	if err := setConnDeadline(incoming, TunnelIdleTimeout()); err != nil {
		log.Printf("set socks5 bind incoming deadline: %v", err)
		return
	}

	Tunnel(client, nil, incoming)
}

// handleSOCKS5UDPAssociate 实现 SOCKS5 UDP ASSOCIATE 命令。
// 服务器监听一个 UDP 端口，接收来自客户端的数据报，去掉 SOCKS5 UDP 头后转发，
// 并将目标回复加上 SOCKS5 UDP 头返回客户端。
func handleSOCKS5UDPAssociate(client net.Conn, clientReader *bufio.Reader, target string) {
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
		log.Printf("socks5 udp associate listen: %v", err)
		socks5Reply(client, 0x01)
		return
	}
	defer conn.Close()

	if !socks5ReplyWithAddr(client, 0x00, conn.LocalAddr().String()) {
		return
	}

	relay := &udpRelay{
		clientConn:    client,
		clientUDPAddr: nil,
		ln:            conn,
		sessions:      make(map[string]*udpSession),
		quit:          make(chan struct{}),
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

// TunnelReadWrite 在 a 与 b 之间双向转发，使用 bufio.Reader 读取 a 侧数据。
// 用于 BIND 场景，因为客户端侧已经经过认证和请求阶段，后续读写字节流可直接转发。
func TunnelReadWrite(a net.Conn, aReader *bufio.Reader, b net.Conn) {
	if aReader != nil && aReader.Buffered() > 0 {
		if _, err := aReader.WriteTo(b); err != nil {
			log.Printf("tunnel buffered write: %v", err)
			return
		}
	}
	Tunnel(a, nil, b)
}

// readSOCKS5UDPHeader 解析 SOCKS5 UDP 请求头，返回目标地址和负载起始偏移。
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
	sessions      map[string]*udpSession
	mu            sync.Mutex
	quit          chan struct{}
}

func (r *udpRelay) run(clientReader *bufio.Reader) {
	// 读取客户端控制连接，一旦关闭则退出 UDP 中继。
	go func() {
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

	buf := make([]byte, 65535)
	for {
		if err := r.ln.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			log.Printf("udp relay set read deadline: %v", err)
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
				log.Printf("udp relay read: %v", err)
				r.closeSessions()
				return
			}
		}

		if r.clientUDPAddr == nil {
			r.clientUDPAddr = srcAddr
		} else if r.clientUDPAddr.IP.String() != srcAddr.IP.String() {
			log.Printf("udp relay ignore packet from %s, expected %s", srcAddr, r.clientUDPAddr)
			continue
		}

		target, frag, dataOff, ok := readSOCKS5UDPHeader(buf[:n])
		if !ok {
			log.Printf("udp relay invalid header from %s", srcAddr)
			continue
		}
		if frag != 0 {
			log.Printf("udp relay fragment %d not supported", frag)
			continue
		}
		data := buf[dataOff:n]

		if err := r.forwardToTarget(target, data); err != nil {
			log.Printf("udp relay forward to %s: %v", target, err)
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
		log.Printf("udp relay build header: %v", err)
		return
	}
	for {
		if err := sess.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			log.Printf("udp session set read deadline: %v", err)
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
				log.Printf("udp session read: %v", err)
				return
			}
		}
		if r.clientUDPAddr == nil {
			continue
		}
		packet := append(header, buf[:n]...)
		if _, err := r.ln.WriteToUDP(packet, r.clientUDPAddr); err != nil {
			log.Printf("udp relay write back to client: %v", err)
			return
		}
		sess.lastUsed = time.Now()
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
