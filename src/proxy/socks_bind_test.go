package proxy

import (
	"bufio"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

func startTestSOCKS5Server(t *testing.T, router *Router) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	quit := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-quit:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				br := bufio.NewReader(c)
				HandleSOCKS5(c, br, nil, nil, router, nil)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		close(quit)
		ln.Close()
	}
}

// startTestProxyWithUpstream 启动一个带 SOCKS5 上游的 k5pw 风格代理服务器，用于测试级联。
func startTestProxyWithUpstream(t *testing.T, upstreamAddr string) (string, func()) {
	t.Helper()
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "upstream", URL: "socks5://" + upstreamAddr},
	})
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	router, err := NewRouter(RoutingConfig{Final: ActionDirect})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	quit := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-quit:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				br := bufio.NewReader(c)
				HandleSOCKS5(c, br, nil, mgr, router, nil)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		close(quit)
		ln.Close()
	}
}

func TestSOCKS5Connect(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect})
	addr, stop := startTestSOCKS5Server(t, router)
	defer stop()

	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write auth greeting: %v", err)
	}
	var authResp [2]byte
	if _, err := client.Read(authResp[:]); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth response = %#v", authResp)
	}

	// Start a target echo server
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	defer echoLn.Close()
	go func() {
		c, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		c.Write(buf[:n])
	}()

	target := echoLn.Addr().String()
	host, port, _ := net.SplitHostPort(target)
	portNum, _ := strconv.Atoi(port)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, net.ParseIP(host).To4()...)
	req = append(req, byte(portNum>>8), byte(portNum))
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write connect request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := client.Read(reply); err != nil {
		t.Fatalf("read connect reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("connect reply = %#v", reply)
	}

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatalf("write data: %v", err)
	}
	buf := make([]byte, 64)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Fatalf("tunneled data = %q, want %q", got, "hello")
	}
}

func TestSOCKS5Bind(t *testing.T) {
	addr, stop := startTestSOCKS5Server(t, nil)
	defer stop()

	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer client.Close()

	// 无认证协商
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write auth greeting: %v", err)
	}
	var authResp [2]byte
	if _, err := client.Read(authResp[:]); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth response = %#v", authResp)
	}

	// 发送 BIND 请求，目标地址为预期连接来源（这里用 127.0.0.1:0 占位）
	req := []byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write bind request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := client.Read(reply); err != nil {
		t.Fatalf("read first bind reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("first bind reply = %#v", reply)
	}
	if reply[3] != 0x01 {
		t.Fatalf("first bind reply address type = %d, want IPv4", reply[3])
	}
	bindIP := net.IP(reply[4:8])
	bindPort := int(binary.BigEndian.Uint16(reply[8:10]))
	bindAddr := net.JoinHostPort(bindIP.String(), itoa(bindPort))

	// 从外部连接 bind 地址
	go func() {
		time.Sleep(50 * time.Millisecond)
		c, err := net.Dial("tcp", bindAddr)
		if err != nil {
			t.Errorf("dial bind address: %v", err)
			return
		}
		defer c.Close()
		if _, err := c.Write([]byte("hello from peer")); err != nil {
			t.Errorf("peer write: %v", err)
		}
	}()

	// 读取第二次回复
	reply2 := make([]byte, 10)
	if _, err := client.Read(reply2); err != nil {
		t.Fatalf("read second bind reply: %v", err)
	}
	if reply2[0] != 0x05 || reply2[1] != 0x00 {
		t.Fatalf("second bind reply = %#v", reply2)
	}

	buf := make([]byte, 64)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read tunneled data: %v", err)
	}
	if got := string(buf[:n]); got != "hello from peer" {
		t.Fatalf("tunneled data = %q, want %q", got, "hello from peer")
	}
}

func TestSOCKS5BindViaUpstream(t *testing.T) {
	upstreamAddr, stopUpstream := startTestSOCKS5Server(t, nil)
	defer stopUpstream()

	proxyAddr, stopProxy := startTestProxyWithUpstream(t, upstreamAddr)
	defer stopProxy()

	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write auth greeting: %v", err)
	}
	var authResp [2]byte
	if _, err := client.Read(authResp[:]); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth response = %#v", authResp)
	}

	req := []byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write bind request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := client.Read(reply); err != nil {
		t.Fatalf("read first bind reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("first bind reply = %#v", reply)
	}
	if reply[3] != 0x01 {
		t.Fatalf("first bind reply address type = %d, want IPv4", reply[3])
	}
	bindIP := net.IP(reply[4:8])
	bindPort := int(binary.BigEndian.Uint16(reply[8:10]))
	bindAddr := net.JoinHostPort(bindIP.String(), itoa(bindPort))

	go func() {
		time.Sleep(50 * time.Millisecond)
		c, err := net.Dial("tcp", bindAddr)
		if err != nil {
			t.Errorf("dial bind address: %v", err)
			return
		}
		defer c.Close()
		if _, err := c.Write([]byte("hello via upstream")); err != nil {
			t.Errorf("peer write: %v", err)
		}
	}()

	reply2 := make([]byte, 10)
	if _, err := client.Read(reply2); err != nil {
		t.Fatalf("read second bind reply: %v", err)
	}
	if reply2[0] != 0x05 || reply2[1] != 0x00 {
		t.Fatalf("second bind reply = %#v", reply2)
	}

	buf := make([]byte, 64)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read tunneled data: %v", err)
	}
	if got := string(buf[:n]); got != "hello via upstream" {
		t.Fatalf("tunneled data = %q, want %q", got, "hello via upstream")
	}
}

func TestSOCKS5UDPAssociate(t *testing.T) {
	addr, stop := startTestSOCKS5Server(t, nil)
	defer stop()

	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write auth greeting: %v", err)
	}
	var authResp [2]byte
	if _, err := client.Read(authResp[:]); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth response = %#v", authResp)
	}

	req := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write udp associate request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := client.Read(reply); err != nil {
		t.Fatalf("read udp associate reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("udp associate reply = %#v", reply)
	}
	if reply[3] != 0x01 {
		t.Fatalf("udp associate reply address type = %d, want IPv4", reply[3])
	}
	udpIP := net.IP(reply[4:8])
	udpPort := int(binary.BigEndian.Uint16(reply[8:10]))
	udpAddr := &net.UDPAddr{IP: udpIP, Port: udpPort}

	// 启动目标 UDP echo 服务器
	echoLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo udp: %v", err)
	}
	defer echoLn.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, src, err := echoLn.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := echoLn.WriteTo(buf[:n], src); err != nil {
				return
			}
		}
	}()

	// 创建 UDP 客户端连接
	udpClient, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		t.Fatalf("dial udp relay: %v", err)
	}
	defer udpClient.Close()

	// 构造 SOCKS5 UDP 请求头
	targetUDPAddr := echoLn.LocalAddr().(*net.UDPAddr)
	header, err := buildSOCKS5UDPHeader(targetUDPAddr.String())
	if err != nil {
		t.Fatalf("build udp header: %v", err)
	}
	packet := append(header, []byte("hello udp")...)
	if _, err := udpClient.Write(packet); err != nil {
		t.Fatalf("write udp packet: %v", err)
	}

	buf := make([]byte, 1024)
	udpClient.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := udpClient.Read(buf)
	if err != nil {
		t.Fatalf("read udp response: %v", err)
	}
	_, _, dataOff, ok := readSOCKS5UDPHeader(buf[:n])
	if !ok {
		t.Fatalf("invalid udp response header")
	}
	if got := string(buf[dataOff:n]); got != "hello udp" {
		t.Fatalf("udp response payload = %q, want %q", got, "hello udp")
	}
}

func TestSOCKS5UDPAssociateViaUpstream(t *testing.T) {
	upstreamAddr, stopUpstream := startTestSOCKS5Server(t, nil)
	defer stopUpstream()

	proxyAddr, stopProxy := startTestProxyWithUpstream(t, upstreamAddr)
	defer stopProxy()

	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write auth greeting: %v", err)
	}
	var authResp [2]byte
	if _, err := client.Read(authResp[:]); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Fatalf("auth response = %#v", authResp)
	}

	req := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write udp associate request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := client.Read(reply); err != nil {
		t.Fatalf("read udp associate reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("udp associate reply = %#v", reply)
	}
	if reply[3] != 0x01 {
		t.Fatalf("udp associate reply address type = %d, want IPv4", reply[3])
	}
	udpIP := net.IP(reply[4:8])
	udpPort := int(binary.BigEndian.Uint16(reply[8:10]))
	udpAddr := &net.UDPAddr{IP: udpIP, Port: udpPort}

	// 启动目标 UDP echo 服务器
	echoLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo udp: %v", err)
	}
	defer echoLn.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, src, err := echoLn.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := echoLn.WriteTo(buf[:n], src); err != nil {
				return
			}
		}
	}()

	udpClient, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		t.Fatalf("dial udp relay: %v", err)
	}
	defer udpClient.Close()

	targetUDPAddr := echoLn.LocalAddr().(*net.UDPAddr)
	header, err := buildSOCKS5UDPHeader(targetUDPAddr.String())
	if err != nil {
		t.Fatalf("build udp header: %v", err)
	}
	packet := append(header, []byte("hello udp via upstream")...)
	if _, err := udpClient.Write(packet); err != nil {
		t.Fatalf("write udp packet: %v", err)
	}

	buf := make([]byte, 1024)
	udpClient.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := udpClient.Read(buf)
	if err != nil {
		t.Fatalf("read udp response: %v", err)
	}
	_, _, dataOff, ok := readSOCKS5UDPHeader(buf[:n])
	if !ok {
		t.Fatalf("invalid udp response header")
	}
	if got := string(buf[dataOff:n]); got != "hello udp via upstream" {
		t.Fatalf("udp response payload = %q, want %q", got, "hello udp via upstream")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
