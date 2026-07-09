package proxy

import (
	"net"
	"strconv"
)

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
		Logger().Warnw("write socks5 reply failed", "error", err)
		return false
	}
	return true
}
