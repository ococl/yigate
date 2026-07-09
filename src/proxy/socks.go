package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
)

// HandleSOCKS5 处理 SOCKS5 代理请求，并在 socks5h_redirect 开启时尝试恢复域名
func HandleSOCKS5(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router) {
	defer client.Close()

	if err := socks5NegotiateAuth(client, clientReader, users); err != nil {
		log.Printf("socks5 auth: %v", err)
		return
	}

	cmd, target, rep, err := socks5ReadRequest(clientReader)
	if err != nil {
		log.Printf("socks5 request: %v", err)
		if rep == 0 {
			rep = 0x01
		}
		socks5Reply(client, rep)
		return
	}

	switch cmd {
	case 0x01: // CONNECT
		handleSOCKS5Connect(client, clientReader, target, mgr, router)
	case 0x02: // BIND
		handleSOCKS5Bind(client, target)
	case 0x03: // UDP ASSOCIATE
		handleSOCKS5UDPAssociate(client, clientReader, target)
	default:
		log.Printf("socks5 unsupported command %d", cmd)
		socks5Reply(client, 0x07)
	}
}

func handleSOCKS5Connect(client net.Conn, clientReader *bufio.Reader, target string, mgr *UpstreamManager, router *Router) {
	host, _, err := net.SplitHostPort(target)
	if router != nil && router.Socks5HEnabled() && err == nil && net.ParseIP(host) != nil {
		log.Printf("socks5 ip target %s will be handled with sniff/rewrite", target)
		handleSOCKS5ConnectWithSniff(client, target, mgr, router)
		return
	}

	targetConn, err := dialTargetForRequest(target, mgr, router)
	if err != nil {
		log.Printf("socks5 connect to %s failed: %v", target, err)
		socks5Reply(client, 0x01)
		return
	}
	if err := setConnDeadline(client, TunnelIdleTimeout()); err != nil {
		log.Printf("set socks5 client deadline: %v", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, TunnelIdleTimeout()); err != nil {
		log.Printf("set socks5 target deadline: %v", err)
		targetConn.Close()
		return
	}

	socks5Reply(client, 0x00)
	Tunnel(client, clientReader, targetConn)
}

// HandleSOCKS is retained as a public alias for callers that use the old name.
func HandleSOCKS(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager, router *Router) {
	HandleSOCKS5(client, clientReader, users, mgr, router)
}

func socks5NegotiateAuth(client net.Conn, r *bufio.Reader, users map[string]string) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return errors.New("not socks5")
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(r, methods); err != nil {
		return err
	}

	if AuthRequired(users) {
		if !contains(methods, 0x02) {
			client.Write([]byte{0x05, 0xFF})
			return errors.New("no acceptable auth method")
		}
		if _, err := client.Write([]byte{0x05, 0x02}); err != nil {
			return err
		}
		return socks5ServerAuth(client, r, users)
	}

	if !contains(methods, 0x00) {
		client.Write([]byte{0x05, 0xFF})
		return errors.New("no acceptable auth method")
	}
	_, err := client.Write([]byte{0x05, 0x00})
	return err
}

func socks5ServerAuth(client net.Conn, r *bufio.Reader, users map[string]string) error {
	ver := make([]byte, 1)
	if _, err := io.ReadFull(r, ver); err != nil || ver[0] != 0x01 {
		return errors.New("invalid auth version")
	}
	ulen := make([]byte, 1)
	if _, err := io.ReadFull(r, ulen); err != nil {
		return err
	}
	uname := make([]byte, ulen[0])
	if _, err := io.ReadFull(r, uname); err != nil {
		return err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(r, plen); err != nil {
		return err
	}
	passwd := make([]byte, plen[0])
	if _, err := io.ReadFull(r, passwd); err != nil {
		return err
	}
	if !Authenticate(string(uname), string(passwd), users) {
		client.Write([]byte{0x01, 0x01})
		return errors.New("socks5 auth failed")
	}
	_, err := client.Write([]byte{0x01, 0x00})
	return err
}

func socks5ReadRequest(r *bufio.Reader) (cmd byte, target string, rep byte, err error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, "", 0x01, err
	}
	if header[0] != 0x05 {
		return 0, "", 0x01, errors.New("invalid version")
	}
	cmd = header[1]
	if header[2] != 0x00 {
		return 0, "", 0x01, errors.New("invalid reserved byte")
	}

	var host string
	switch header[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return 0, "", 0x01, err
		}
		host = net.IP(ip).String()
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return 0, "", 0x01, err
		}
		domain := make([]byte, l[0])
		if _, err := io.ReadFull(r, domain); err != nil {
			return 0, "", 0x01, err
		}
		host = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(r, ip); err != nil {
			return 0, "", 0x01, err
		}
		host = net.IP(ip).String()
	default:
		return 0, "", 0x08, fmt.Errorf("unsupported address type %d", header[3])
	}

	port := make([]byte, 2)
	if _, err := io.ReadFull(r, port); err != nil {
		return 0, "", 0x01, err
	}
	p := int(port[0])<<8 | int(port[1])
	return cmd, net.JoinHostPort(host, strconv.Itoa(p)), 0x00, nil
}

func socks5Reply(client net.Conn, rep byte) {
	_ = socks5ReplyWithAddr(client, rep, "0.0.0.0:0")
}

func socks5ReplyWithAddr(client net.Conn, rep byte, addr string) bool {
	if addr == "" {
		addr = "0.0.0.0:0"
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host = "0.0.0.0"
		portStr = "0"
	}
	// 0.0.0.0/:: 对客户端无意义，回复 127.0.0.1 便于本地测试与使用。
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 0
	}

	var reply []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			reply = []byte{0x05, rep, 0x00, 0x01}
			reply = append(reply, ip4...)
		} else {
			reply = []byte{0x05, rep, 0x00, 0x04}
			reply = append(reply, ip.To16()...)
		}
	} else {
		reply = []byte{0x05, rep, 0x00, 0x03, byte(len(host))}
		reply = append(reply, []byte(host)...)
	}
	reply = append(reply, byte(port>>8), byte(port))
	if _, err := client.Write(reply); err != nil {
		log.Printf("write socks5 reply: %v", err)
		return false
	}
	return true
}

func contains(b []byte, v byte) bool {
	for _, x := range b {
		if x == v {
			return true
		}
	}
	return false
}
