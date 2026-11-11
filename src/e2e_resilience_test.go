package main

// 代理稳健性的端到端验收测试。
//
// 与单元测试不同，这里启动真实的 Server（真实监听、真实协议处理），
// 用真实的 TCP/UDP 客户端从外部验证行为，覆盖故障场景：
// UDP 关联在握手超时后仍存活、UDP 会话数有上限、级联 BIND 不永久挂起、
// 回退不重复回复、半关闭不截断响应等。

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"yigate/proxy"
)

const e2eListenAddr = "127.0.0.1:0"

// startE2EServer 启动一个真实 Server，返回其监听地址。
func startE2EServer(t *testing.T, mutate func(*Config)) (string, *Server) {
	t.Helper()
	cfg := defaultConfig()
	cfg.Listen = ""
	cfg.Listeners = []ListenerConfig{{
		Listen: e2eListenAddr,
		Socks5: Socks5Config{AllowBind: true, AllowUDPAssociate: true},
	}}
	cfg.Management = ""
	cfg.AllowAnonymousProxy = true
	cfg.AllowPrivateTargets = true
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.Upstreams = nil
	cfg.CheckInterval = 0
	cfg.Routing.Final = "direct"
	cfg.Routing.Socks5HRedirect.Enabled = false
	cfg.Routing.GeoSite.AutoUpdate = false
	cfg.Socks5.AllowBind = true
	cfg.Socks5.AllowUDPAssociate = true
	if mutate != nil {
		mutate(cfg)
	}
	s, err := NewServer("", cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = s.Start() }()
	t.Cleanup(s.Stop)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ln := s.currentListener(e2eListenAddr); ln != nil {
			return ln.Addr().String(), s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server listener did not become ready")
	return "", nil
}

// socks5Connect 完成一次 SOCKS5 握手（无认证）并发出命令，返回连接与 bufio reader。
func socks5Connect(t *testing.T, addr string, cmd byte, target string) (net.Conn, *bufio.Reader, []byte) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	br := bufio.NewReader(conn)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(br, resp); err != nil {
		t.Fatalf("read greeting: %v", err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		t.Fatalf("unexpected greeting reply: %x", resp)
	}
	req := []byte{0x05, cmd, 0x00}
	req = append(req, socks5Addr(target)...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	return conn, br, nil
}

// socks5Addr 编码 host:port 为 SOCKS5 地址段（域名形式）。
func socks5Addr(target string) []byte {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return []byte{0x01, 0, 0, 0, 0, 0, 0}
	}
	port, _ := net.LookupPort("tcp", portStr)
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			out := []byte{0x01}
			out = append(out, v4...)
			return append(out, byte(port>>8), byte(port))
		}
		out := []byte{0x04}
		out = append(out, ip.To16()...)
		return append(out, byte(port>>8), byte(port))
	}
	out := []byte{0x03, byte(len(host))}
	out = append(out, host...)
	return append(out, byte(port>>8), byte(port))
}

// readSocks5Reply 读取一个完整 SOCKS5 响应，返回 REP 与绑定地址。
func readSocks5Reply(br *bufio.Reader) (byte, string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(br, head); err != nil {
		return 0, "", err
	}
	if head[0] != 0x05 {
		return 0, "", fmt.Errorf("unexpected socks version %d", head[0])
	}
	var host string
	switch head[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = net.IP(b).String()
	case 0x03:
		l, err := br.ReadByte()
		if err != nil {
			return 0, "", err
		}
		b := make([]byte, int(l))
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = string(b)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = net.IP(b).String()
	default:
		return 0, "", fmt.Errorf("unexpected atyp %d", head[3])
	}
	var port uint16
	if err := binary.Read(br, binary.BigEndian, &port); err != nil {
		return 0, "", err
	}
	return head[1], net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

// startUDPEchoServer 启动一个 UDP echo 服务，把收到的数据原样回给来源。
func startUDPEchoServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp echo: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buf[:n], src)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

// udpAssociate 建立 UDP 关联，返回中继地址与用于收发的 UDP socket。
func udpAssociate(t *testing.T, proxyAddr string) (net.Conn, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
	ctrl, br, _ := socks5Connect(t, proxyAddr, 0x03, "0.0.0.0:0")
	rep, bindAddr, err := readSocks5Reply(br)
	if err != nil {
		t.Fatalf("read udp associate reply: %v", err)
	}
	if rep != 0x00 {
		t.Fatalf("udp associate refused: rep=0x%02x", rep)
	}
	host, portStr, _ := net.SplitHostPort(bindAddr)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	relayAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, portStr))
	if err != nil {
		t.Fatalf("resolve relay addr: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen client udp: %v", err)
	}
	t.Cleanup(func() { udpConn.Close() })
	t.Cleanup(func() { ctrl.Close() })
	return ctrl, udpConn, relayAddr
}

func buildUDPPacket(target *net.UDPAddr, payload []byte) []byte {
	pkt := []byte{0x00, 0x00, 0x00}
	pkt = append(pkt, socks5Addr(target.String())...)
	return append(pkt, payload...)
}

func readUDPPacket(conn *net.UDPConn, timeout time.Duration) ([]byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65535)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	if n < 4 {
		return nil, errors.New("short udp packet")
	}
	atyp := buf[3]
	headLen := 4
	switch atyp {
	case 0x01:
		headLen += 4
	case 0x03:
		headLen += 1 + int(buf[4])
	case 0x04:
		headLen += 16
	}
	headLen += 2
	if n < headLen {
		return nil, errors.New("short udp header")
	}
	return buf[headLen:n], nil
}

// TestE2EUDPAssociateSurvivesHandshakeTimeout 验收：UDP 关联不能被握手超时误杀。
// 握手超时设为 500ms，然后在 2s 内持续做 UDP echo，全部必须成功。
func TestE2EUDPAssociateSurvivesHandshakeTimeout(t *testing.T) {
	echoAddr := startUDPEchoServer(t)
	proxyAddr, _ := startE2EServer(t, func(cfg *Config) {
		cfg.Timeouts.Handshake = proxy.Duration(500 * time.Millisecond)
	})

	_, udpConn, relayAddr := udpAssociate(t, proxyAddr)

	// 覆盖握手超时前后的多个时间点；旧实现在 500ms 后中继被关闭。
	for i := 0; i < 6; i++ {
		payload := []byte(fmt.Sprintf("ping-%d", i))
		if _, err := udpConn.WriteToUDP(buildUDPPacket(echoAddr, payload), relayAddr); err != nil {
			t.Fatalf("send udp packet %d: %v", i, err)
		}
		got, err := readUDPPacket(udpConn, time.Second)
		if err != nil {
			t.Fatalf("udp echo %d failed after %v (handshake timeout is 500ms): %v",
				i, time.Duration(i)*350*time.Millisecond, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("udp echo %d payload mismatch: got %q want %q", i, got, payload)
		}
		time.Sleep(350 * time.Millisecond)
	}
}
