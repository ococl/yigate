package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

const socks5SniffTimeout = 2 * time.Second

func handleSOCKS5ConnectWithSniff(client net.Conn, target string, mgr *UpstreamManager, router *Router, access *AccessController, user string, sourceIP ...string) {
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	if !shouldSniffSOCKS5Target(target, mgr, router) {
		remote, err := dialTargetForRequest(target, mgr, router, sip)
		if err != nil {
			writeSOCKS5ConnectReply(client, SOCKS5ReplyFromError(err))
			return
		}
		defer remote.Close()
		if !writeSOCKS5ConnectReply(client, 0x00) {
			return
		}
		Tunnel(client, nil, remote, newTrafficKey(user, remote, target), getGlobalTrafficAccounter())
		return
	}

	// socks5:// clients often send an IP after local DNS resolution. Reply first,
	// then recover the domain from TLS SNI or HTTP Host and redial by name.
	if !writeSOCKS5ConnectReply(client, 0x00) {
		return
	}
	initial := readInitialClientBytes(client)
	rewritten := rewriteSOCKS5TargetFromInitial(target, initial)
	Logger().Infow("socks5 sniff target", "target", target, "rewritten", rewritten, "initial_bytes", len(initial))
	remote, err := dialSOCKS5RecoveredTarget(rewritten, target, mgr, router, sip)
	if err != nil {
		Logger().Warnw("socks5 connect failed", "target", rewritten, "error", err)
		client.Close()
		return
	}
	defer remote.Close()
	Logger().Infow("socks5 connected", "target", rewritten)
	if len(initial) > 0 {
		if _, err := remote.Write(initial); err != nil {
			Logger().Warnw("socks5 write initial failed", "target", rewritten, "error", err)
			client.Close()
			return
		}
	}
	Tunnel(client, nil, remote, newTrafficKey(user, remote, rewritten), getGlobalTrafficAccounter())
}

func dialSOCKS5RecoveredTarget(rewritten string, original string, mgr *UpstreamManager, router *Router, sourceIP ...string) (net.Conn, error) {
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	if rewritten != original {
		// 优先使用域名本身走路由（socks5h / domain_suffix 规则），避免本地 DNS 污染
		// 导致解析到虚假 IP 后 TCP 握手成功但无响应。
		if conn, err := dialTargetForRequest(rewritten, mgr, router, sip); err == nil {
			Logger().Infow("socks5 dial domain succeeded", "target", rewritten)
			return conn, nil
		}
		// 域名路由失败后再尝试本地解析兜底。
		if conn, err := dialLocalResolvedTarget(rewritten, mgr, router, sip); err == nil {
			Logger().Infow("socks5 local re-resolve succeeded", "target", rewritten)
			return conn, nil
		}
	}
	return dialTargetForRequest(original, mgr, router, sip)
}

func dialLocalResolvedTarget(target string, mgr *UpstreamManager, router *Router, sourceIP ...string) (net.Conn, error) {
	sip := ""
	if len(sourceIP) > 0 {
		sip = sourceIP[0]
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(host) != nil {
		return nil, fmt.Errorf("target %s is not a domain", target)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := router.lookupIPAddr(ctx, host)
	if err != nil {
		Logger().Warnw("socks5 local resolve failed", "host", host, "error", err)
		return nil, err
	}
	Logger().Infow("socks5 local resolve succeeded", "host", host, "addrs", addrs)
	var lastErr error
	prec := IPPrecedenceIPv4First
	if router != nil {
		prec = router.ipPrecedence
	}
	for _, ip := range sortAddrsByPrecedence(addrs, prec) {
		dialTarget := net.JoinHostPort(ip.String(), port)
		conn, err := dialTargetForRequest(dialTarget, mgr, router, sip)
		if err == nil {
			Logger().Infow("socks5 dial succeeded", "target", dialTarget)
			return conn, nil
		}
		lastErr = err
		Logger().Warnw("socks5 dial failed", "target", dialTarget, "error", err)
	}
	if lastErr == nil {
		lastErr = &net.DNSError{Err: "no addresses", Name: host}
	}
	return nil, lastErr
}

// sortAddrsByPrecedence 根据 IP 优先级策略对 DNS 解析结果排序。
// ipv4_first（默认）：IPv4 优先，IPv6 兜底
// ipv6_first：IPv6 优先，IPv4 兜底
// system：保持 DNS 返回顺序
func sortAddrsByPrecedence(addrs []net.IPAddr, precedence string) []net.IP {
	switch precedence {
	case IPPrecedenceSystem:
		ordered := make([]net.IP, 0, len(addrs))
		for _, addr := range addrs {
			if ip := addr.IP.To16(); ip != nil {
				ordered = append(ordered, ip)
			}
		}
		return ordered
	case IPPrecedenceIPv6First:
		ordered := make([]net.IP, 0, len(addrs))
		for _, addr := range addrs {
			if addr.IP.To4() == nil {
				if ip16 := addr.IP.To16(); ip16 != nil {
					ordered = append(ordered, ip16)
				}
			}
		}
		for _, addr := range addrs {
			if ip4 := addr.IP.To4(); ip4 != nil {
				ordered = append(ordered, ip4)
			}
		}
		return ordered
	default: // ipv4_first
		ordered := make([]net.IP, 0, len(addrs))
		for _, addr := range addrs {
			if ip4 := addr.IP.To4(); ip4 != nil {
				ordered = append(ordered, ip4)
			}
		}
		for _, addr := range addrs {
			if addr.IP.To4() == nil {
				if ip16 := addr.IP.To16(); ip16 != nil {
					ordered = append(ordered, ip16)
				}
			}
		}
		return ordered
	}
}

func shouldSniffSOCKS5Target(target string, mgr *UpstreamManager, router *Router) bool {
	if mgr == nil || router == nil || !router.socks5h.Enabled {
		return false
	}
	if mgr.Socks5AttemptCount(router.socks5h.Upstreams) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return false
	}
	return net.ParseIP(host) != nil
}

func writeSOCKS5ConnectReply(conn net.Conn, rep byte) bool {
	// 根据连接的本地地址族选择回复地址类型，避免 IPv6 客户端收到 IPv4 回复。
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			// IPv6 连接：使用 IPv6 地址类型 (0x04)
			_, err := conn.Write([]byte{0x05, rep, 0x00, 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
			return err == nil
		}
	}
	// IPv4 连接或无法确定：使用 IPv4 地址类型 (0x01)
	_, err = conn.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err == nil
}

func readInitialClientBytes(conn net.Conn) []byte {
	_ = conn.SetReadDeadline(time.Now().Add(socks5SniffTimeout))
	defer conn.SetReadDeadline(time.Time{})

	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 2048)
	for len(buf) < cap(buf) {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if hasEnoughInitialBytes(buf) || err != nil {
			break
		}
	}
	return buf
}

func hasEnoughInitialBytes(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	if buf[0] == 0x16 {
		if len(buf) < 5 {
			return false
		}
		recordLen := int(binary.BigEndian.Uint16(buf[3:5]))
		return recordLen > 0 && len(buf) >= 5+recordLen
	}
	text := string(buf)
	if strings.Contains(text, "\r\n\r\n") || strings.Contains(text, "\n\n") {
		return true
	}
	if !(strings.HasPrefix(text, "GET ") || strings.HasPrefix(text, "POST ") || strings.HasPrefix(text, "HEAD ") || strings.HasPrefix(text, "PUT ") || strings.HasPrefix(text, "DELETE ") || strings.HasPrefix(text, "OPTIONS ") || strings.HasPrefix(text, "PATCH ")) {
		return true
	}
	return false
}

func rewriteSOCKS5TargetFromInitial(target string, initial []byte) string {
	if len(initial) == 0 {
		return target
	}
	_, port, err := net.SplitHostPort(target)
	if err != nil {
		return target
	}
	host := sniffTLSServerName(initial)
	if host == "" {
		host = sniffHTTPHost(initial)
	}
	if host == "" || net.ParseIP(host) != nil {
		return target
	}
	return net.JoinHostPort(host, port)
}

func sniffHTTPHost(data []byte) string {
	text := string(data)
	if !(strings.HasPrefix(text, "GET ") || strings.HasPrefix(text, "POST ") || strings.HasPrefix(text, "HEAD ") || strings.HasPrefix(text, "PUT ") || strings.HasPrefix(text, "DELETE ") || strings.HasPrefix(text, "OPTIONS ") || strings.HasPrefix(text, "PATCH ")) {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) >= 5 && strings.EqualFold(line[:5], "host:") {
			host := strings.TrimSpace(line[5:])
			if hostOnly, _, err := net.SplitHostPort(host); err == nil {
				host = hostOnly
			}
			return strings.Trim(host, "[]")
		}
	}
	return ""
}

func sniffTLSServerName(data []byte) string {
	if len(data) < 5 || data[0] != 0x16 {
		return ""
	}
	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	if recordLen <= 0 || len(data) < 5+recordLen {
		return ""
	}
	handshake := data[5 : 5+recordLen]
	if len(handshake) < 42 || handshake[0] != 0x01 {
		return ""
	}
	handshakeLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if handshakeLen+4 > len(handshake) {
		return ""
	}
	body := handshake[4 : 4+handshakeLen]
	pos := 2 + 32
	if pos >= len(body) {
		return ""
	}
	sessionLen := int(body[pos])
	pos += 1 + sessionLen
	if pos+2 > len(body) {
		return ""
	}
	cipherLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2 + cipherLen
	if pos >= len(body) {
		return ""
	}
	compressionLen := int(body[pos])
	pos += 1 + compressionLen
	if pos+2 > len(body) {
		return ""
	}
	extLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2
	if pos+extLen > len(body) {
		return ""
	}
	extensions := body[pos : pos+extLen]
	for len(extensions) >= 4 {
		extType := binary.BigEndian.Uint16(extensions[0:2])
		extDataLen := int(binary.BigEndian.Uint16(extensions[2:4]))
		if 4+extDataLen > len(extensions) {
			return ""
		}
		extData := extensions[4 : 4+extDataLen]
		if extType == 0x0000 {
			return parseTLSServerNameExtension(extData)
		}
		extensions = extensions[4+extDataLen:]
	}
	return ""
}

func parseTLSServerNameExtension(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	listLen := int(binary.BigEndian.Uint16(data[0:2]))
	if listLen+2 > len(data) {
		return ""
	}
	list := data[2 : 2+listLen]
	for len(list) >= 3 {
		nameType := list[0]
		nameLen := int(binary.BigEndian.Uint16(list[1:3]))
		if 3+nameLen > len(list) {
			return ""
		}
		if nameType == 0 {
			name := strings.ToLower(strings.TrimSpace(string(list[3 : 3+nameLen])))
			if name != "" {
				return name
			}
		}
		list = list[3+nameLen:]
	}
	return ""
}
