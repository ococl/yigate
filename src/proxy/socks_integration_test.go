package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func startMockSocks5ProxyWithUsers(t *testing.T, users map[string]string) string {
	t.Helper()
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				HandleSOCKS5(c, br, users, nil, router, nil)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func socks5Connect(t *testing.T, proxy, target string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		t.Fatalf("write methods: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		conn.Close()
		t.Fatalf("auth handshake failed: %v, %v", err, resp)
	}

	req := []byte{0x05, 0x01, 0x00, 0x01}
	ip := net.ParseIP("127.0.0.1").To4()
	req = append(req, ip...)
	port := uint16(portOf(t, target))
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		t.Fatalf("write request: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != 0x00 {
		conn.Close()
		t.Fatalf("connect failed: code %d", reply[1])
	}
	return conn
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func TestHandleSOCKS5AnonymousConnect(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, nil)
	target := startMockTarget(t)
	conn := socks5Connect(t, proxyAddr, target)
	defer conn.Close()

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestHandleSOCKS5AuthConnect(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, map[string]string{"user": "pass"})
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil || resp[1] != 0x02 {
		t.Fatalf("expected user/pass auth, got %v", resp)
	}
	// username
	auth := []byte{0x01, 0x04, 'u', 's', 'e', 'r', 0x04, 'p', 'a', 's', 's'}
	if _, err := conn.Write(auth); err != nil {
		t.Fatal(err)
	}
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResp); err != nil || authResp[1] != 0x00 {
		t.Fatalf("auth failed: %v", authResp)
	}

	target := startMockTarget(t)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, net.ParseIP("127.0.0.1").To4()...)
	port := uint16(portOf(t, target))
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[1] != 0x00 {
		t.Fatalf("connect failed: %v", err)
	}

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("echo failed: %v, %q", err, buf[:n])
	}
}

func TestHandleSOCKS5AuthFailed(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, map[string]string{"user": "pass"})
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	io.ReadFull(conn, resp)
	if resp[1] != 0x02 {
		t.Fatalf("expected auth method 0x02, got %d", resp[1])
	}
	auth := []byte{0x01, 0x04, 'u', 's', 'e', 'r', 0x04, 'w', 'r', 'o', 'n', 'g'}
	conn.Write(auth)
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResp); err != nil {
		t.Fatalf("read auth resp: %v", err)
	}
	if authResp[1] != 0x01 {
		t.Fatalf("expected auth failure 0x01, got %d", authResp[1])
	}
}

func TestHandleSOCKS5BindCommand(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, nil)
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// auth
	conn.Write([]byte{0x05, 0x01, 0x00})
	resp := make([]byte, 2)
	io.ReadFull(conn, resp)

	req := []byte{0x05, 0x02, 0x00, 0x01}
	req = append(req, net.ParseIP("127.0.0.1").To4()...)
	req = append(req, 0, 0)
	conn.Write(req)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read bind reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("bind failed: %d", reply[1])
	}
}

func TestHandleSOCKS5UDPCommand(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, nil)
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte{0x05, 0x01, 0x00})
	resp := make([]byte, 2)
	io.ReadFull(conn, resp)

	req := []byte{0x05, 0x03, 0x00, 0x01}
	req = append(req, net.ParseIP("127.0.0.1").To4()...)
	req = append(req, 0, 0)
	conn.Write(req)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read udp reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("udp associate failed: %d", reply[1])
	}
}

func TestHandleSOCKS5UnsupportedCommand(t *testing.T) {
	proxyAddr := startMockSocks5ProxyWithUsers(t, nil)
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte{0x05, 0x01, 0x00})
	resp := make([]byte, 2)
	io.ReadFull(conn, resp)

	req := []byte{0x05, 0x04, 0x00, 0x01}
	req = append(req, net.ParseIP("127.0.0.1").To4()...)
	req = append(req, 0, 0)
	conn.Write(req)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != 0x07 {
		t.Fatalf("expected command not supported 0x07, got %d", reply[1])
	}
}
