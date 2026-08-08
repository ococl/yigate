package proxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// dialSOCKS5 通过 SOCKS5 上游连接到目标
func (u *Upstream) dialSOCKS5(target string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddress(u.urlForType(ProxyTypeSocks5)), ProxyDialTimeout())
	if err != nil {
		return nil, err
	}
	if err := setConnDeadline(conn, ProxyHandshakeTimeout()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := socks5ClientHandshake(conn, u); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := socks5SendRequest(conn, 0x01, target); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func socks5Auth(conn net.Conn, username, password string) error {
	if len(username) > 255 || len(password) > 255 {
		return errors.New("socks5 username/password too long")
	}
	buf := make([]byte, 0, 513)
	buf = append(buf, 0x01, byte(len(username)))
	buf = append(buf, []byte(username)...)
	buf = append(buf, byte(len(password)))
	buf = append(buf, []byte(password)...)
	if _, err := conn.Write(buf); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}
	if resp[0] != 0x01 || resp[1] != 0x00 {
		return errors.New("socks5 auth failed")
	}
	return nil
}

// socks5ClientHandshake 完成 SOCKS5 方法协商与用户名密码认证。
func socks5ClientHandshake(conn net.Conn, u *Upstream) error {
	user := ""
	pass := ""
	if u.URL != nil && u.URL.User != nil {
		user = u.URL.User.Username()
		pass, _ = u.URL.User.Password()
	}
	methods := []byte{0x00}
	if user != "" || pass != "" {
		if len(user) > 255 || len(pass) > 255 {
			return newDialError(ErrCategoryUpstream, "auth", errors.New("socks5 credentials too long"))
		}
		methods = append(methods, 0x02)
	}
	greeting := []byte{0x05, byte(len(methods))}
	greeting = append(greeting, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return newDialError(ErrCategoryUpstream, "handshake", err)
	}
	var methodResp [2]byte
	if _, err := io.ReadFull(conn, methodResp[:]); err != nil {
		return newDialError(ErrCategoryUpstream, "handshake", err)
	}
	if methodResp[0] != 0x05 || methodResp[1] == 0xff {
		return newDialError(ErrCategoryUpstream, "handshake", errors.New("socks5 auth method rejected"))
	}
	if methodResp[1] == 0x02 {
		if err := socks5Auth(conn, user, pass); err != nil {
			return newDialError(ErrCategoryUpstream, "auth", err)
		}
	}
	return nil
}

// socks5SendRequest 向已认证连接的 SOCKS5 服务器发送 cmd 请求并读取第一次回复。
// 返回回复中的地址字符串以及错误。
func socks5SendRequest(conn net.Conn, cmd byte, target string) (string, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return "", fmt.Errorf("invalid target port %q", portStr)
	}
	addr, err := socks5AddressBytes(host)
	if err != nil {
		return "", err
	}
	packet := []byte{0x05, cmd, 0x00}
	packet = append(packet, addr...)
	packet = append(packet, byte(port>>8), byte(port))
	if _, err := conn.Write(packet); err != nil {
		return "", err
	}

	var reply [4]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return "", err
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return "", classifySOCKS5Reply(reply[1], cmd)
	}
	replyHost, err := readSOCKS5AddressValue(conn, reply[3])
	if err != nil {
		return "", err
	}
	replyPort, err := readSOCKS5PortValue(conn)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(replyHost, strconv.Itoa(replyPort)), nil
}
