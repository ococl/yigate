package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const k5pwSOCKS5SniffTimeout = 2 * time.Second

func k5pwHandleSOCKS5ConnectWithSniff(client net.Conn, target string, mgr *UpstreamManager, router *Router) {
	if !k5pwShouldSniffSOCKS5Target(target, mgr, router) {
		remote, err := dialTargetForRequest(target, mgr, router)
		if err != nil {
			k5pwWriteSOCKS5ConnectReply(client, 0x05)
			return
		}
		defer remote.Close()
		if !k5pwWriteSOCKS5ConnectReply(client, 0x00) {
			return
		}
		k5pwSOCKS5SniffRelayTCP(client, remote)
		return
	}

	// socks5:// clients often send an IP after local DNS resolution. Reply first,
	// then recover the domain from TLS SNI or HTTP Host and redial by name.
	if !k5pwWriteSOCKS5ConnectReply(client, 0x00) {
		return
	}
	initial := k5pwReadInitialClientBytes(client)
	rewritten := k5pwRewriteSOCKS5TargetFromInitial(target, initial)
	fmt.Printf("[socks5] target=%s rewritten=%s initial=%d\n", target, rewritten, len(initial))
	remote, err := k5pwDialSOCKS5RecoveredTarget(rewritten, target, mgr, router)
	if err != nil {
		fmt.Printf("[socks5] connect to %s failed: %v\n", rewritten, err)
		return
	}
	defer remote.Close()
	fmt.Printf("[socks5] connected to %s\n", rewritten)
	if len(initial) > 0 {
		if _, err := remote.Write(initial); err != nil {
			fmt.Printf("[socks5] write initial to %s failed: %v\n", rewritten, err)
			return
		}
	}
	k5pwSOCKS5SniffRelayTCP(client, remote)
}

func k5pwDialSOCKS5RecoveredTarget(rewritten string, original string, mgr *UpstreamManager, router *Router) (net.Conn, error) {
	if rewritten != original {
		// 优先使用域名本身走路由（socks5h / domain_suffix 规则），避免本地 DNS 污染
		// 导致解析到虚假 IP 后 TCP 握手成功但无响应。
		if conn, err := dialTargetForRequest(rewritten, mgr, router); err == nil {
			fmt.Printf("[socks5] dial domain %s succeeded\n", rewritten)
			return conn, nil
		}
		// 域名路由失败后再尝试本地解析兜底。
		if conn, err := k5pwDialLocalResolvedTarget(rewritten, mgr, router); err == nil {
			fmt.Printf("[socks5] local re-resolve %s succeeded\n", rewritten)
			return conn, nil
		}
	}
	return dialTargetForRequest(original, mgr, router)
}

func k5pwDialLocalResolvedTarget(target string, mgr *UpstreamManager, router *Router) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(host) != nil {
		return nil, fmt.Errorf("target %s is not a domain", target)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		fmt.Printf("[socks5] local resolve %s failed: %v\n", host, err)
		return nil, err
	}
	fmt.Printf("[socks5] local resolve %s addrs=%v\n", host, addrs)
	var lastErr error
	for _, ip := range k5pwPreferIPv4(addrs) {
		dialTarget := net.JoinHostPort(ip.String(), port)
		conn, err := dialTargetForRequest(dialTarget, mgr, router)
		if err == nil {
			fmt.Printf("[socks5] dial %s succeeded\n", dialTarget)
			return conn, nil
		}
		lastErr = err
		fmt.Printf("[socks5] dial %s failed: %v\n", dialTarget, err)
	}
	if lastErr == nil {
		lastErr = &net.DNSError{Err: "no addresses", Name: host}
	}
	return nil, lastErr
}

func k5pwPreferIPv4(addrs []net.IPAddr) []net.IP {
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

func k5pwShouldSniffSOCKS5Target(target string, mgr *UpstreamManager, router *Router) bool {
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

func k5pwWriteSOCKS5ConnectReply(conn net.Conn, rep byte) bool {
	_, err := conn.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err == nil
}

func k5pwReadInitialClientBytes(conn net.Conn) []byte {
	_ = conn.SetReadDeadline(time.Now().Add(k5pwSOCKS5SniffTimeout))
	defer conn.SetReadDeadline(time.Time{})

	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 2048)
	for len(buf) < cap(buf) {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if k5pwHasEnoughInitialBytes(buf) || err != nil {
			break
		}
	}
	return buf
}

func k5pwHasEnoughInitialBytes(buf []byte) bool {
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

func k5pwRewriteSOCKS5TargetFromInitial(target string, initial []byte) string {
	if len(initial) == 0 {
		return target
	}
	_, port, err := net.SplitHostPort(target)
	if err != nil {
		return target
	}
	host := k5pwSniffTLSServerName(initial)
	if host == "" {
		host = k5pwSniffHTTPHost(initial)
	}
	if host == "" || net.ParseIP(host) != nil {
		return target
	}
	return net.JoinHostPort(host, port)
}

func k5pwSniffHTTPHost(data []byte) string {
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

func k5pwSniffTLSServerName(data []byte) string {
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
			return k5pwParseTLSServerNameExtension(extData)
		}
		extensions = extensions[4+extDataLen:]
	}
	return ""
}

func k5pwParseTLSServerNameExtension(data []byte) string {
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

func k5pwSOCKS5SniffRelayTCP(a net.Conn, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
	<-done
}
