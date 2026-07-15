package proxy

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTProxyConfigNormalizedMode(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"", "redirect"},
		{"redirect", "redirect"},
		{"tproxy", "tproxy"},
		{"REDIRECT", "redirect"},
		{"TPROXY", "redirect"}, // 大小写敏感，非 "tproxy" 一律视为 redirect
		{"unknown", "redirect"},
	}
	for _, tt := range tests {
		cfg := TProxyConfig{Enabled: true, Mode: tt.mode}
		if got := cfg.NormalizedMode(); got != tt.want {
			t.Errorf("NormalizedMode() mode=%q, got=%q, want=%q", tt.mode, got, tt.want)
		}
	}
}

func TestTProxyConfigIsTProxyEnabled(t *testing.T) {
	cfg := TProxyConfig{Enabled: true}
	if !cfg.IsTProxyEnabled() {
		t.Error("IsTProxyEnabled() should be true")
	}
	cfg = TProxyConfig{Enabled: false}
	if cfg.IsTProxyEnabled() {
		t.Error("IsTProxyEnabled() should be false")
	}
}

// mockConn 是一个用于测试的 net.Conn 桩。
type mockConn struct {
	net.Conn
	localAddr  net.Addr
	remoteAddr net.Addr
	closeMu    sync.Mutex
	closed     bool
}

func (m *mockConn) LocalAddr() net.Addr  { return m.localAddr }
func (m *mockConn) RemoteAddr() net.Addr { return m.remoteAddr }
func (m *mockConn) Close() error {
	m.closeMu.Lock()
	m.closed = true
	m.closeMu.Unlock()
	if m.Conn != nil {
		return m.Conn.Close()
	}
	return nil
}
func (m *mockConn) Read(p []byte) (int, error) {
	if m.Conn != nil {
		return m.Conn.Read(p)
	}
	return 0, io.EOF
}
func (m *mockConn) Write(p []byte) (int, error) {
	if m.Conn != nil {
		return m.Conn.Write(p)
	}
	return len(p), nil
}
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

type mockAddr struct {
	addr string
}

func (m *mockAddr) Network() string { return "tcp" }
func (m *mockAddr) String() string  { return m.addr }

func TestGetOriginalDestinationTProxyMode(t *testing.T) {
	conn := &mockConn{
		localAddr:  &mockAddr{addr: "93.184.216.34:443"},
		remoteAddr: &mockAddr{addr: "10.0.0.1:12345"},
	}
	target, err := getOriginalDestination(conn, "tproxy")
	if err != nil {
		t.Fatalf("getOriginalDestination tproxy mode failed: %v", err)
	}
	if target != "93.184.216.34:443" {
		t.Errorf("getOriginalDestination tproxy mode got=%q, want=93.184.216.34:443", target)
	}
}

func TestGetOriginalDestinationTProxyModeIPv6(t *testing.T) {
	conn := &mockConn{
		localAddr:  &mockAddr{addr: "[2001:db8::1]:443"},
		remoteAddr: &mockAddr{addr: "[fe80::1]:12345"},
	}
	target, err := getOriginalDestination(conn, "tproxy")
	if err != nil {
		t.Fatalf("getOriginalDestination tproxy mode IPv6 failed: %v", err)
	}
	if target != "[2001:db8::1]:443" {
		t.Errorf("getOriginalDestination tproxy mode IPv6 got=%q, want=[2001:db8::1]:443", target)
	}
}

func TestGetOriginalDestinationTProxyModeNilLocalAddr(t *testing.T) {
	conn := &mockConn{
		localAddr:  nil,
		remoteAddr: &mockAddr{addr: "10.0.0.1:12345"},
	}
	_, err := getOriginalDestination(conn, "tproxy")
	if err == nil {
		t.Error("getOriginalDestination with nil LocalAddr should return error")
	}
}

func TestHandleTProxyTProxyModeDialFail(t *testing.T) {
	// 使用一个不可达的目标地址，验证 HandleTProxy 在拨号失败时正确返回
	conn := &mockConn{
		localAddr:  &mockAddr{addr: "192.0.2.1:9999"}, // TEST-NET-1，不可路由
		remoteAddr: &mockAddr{addr: "10.0.0.1:12345"},
	}

	mgr, err := NewUpstreamManager(nil)
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	router, err := NewRouter(RoutingConfig{
		Final: "direct",
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	// 设置短超时以加速测试
	SetTimeouts(TimeoutConfig{
		TunnelIdle: Duration(2 * time.Second),
	})

	// HandleTProxy 应该在拨号失败后立即返回
	done := make(chan struct{})
	go func() {
		defer close(done)
		HandleTProxy(conn, mgr, router, "tproxy")
	}()

	select {
	case <-done:
		// 预期行为：拨号失败后 HandleTProxy 返回
	case <-time.After(10 * time.Second):
		t.Fatal("HandleTProxy did not return within 10 seconds")
	}

	if !conn.closed {
		t.Error("connection should be closed after HandleTProxy returns")
	}
}

func TestHandleTProxyRedirectModeNoOriginalDst(t *testing.T) {
	// 在非 REDIRECT 环境下，originalDst 应返回错误
	// 使用 mockConn（非 *net.TCPConn），originalDst 应失败
	conn := &mockConn{
		localAddr:  &mockAddr{addr: "127.0.0.1:8080"},
		remoteAddr: &mockAddr{addr: "10.0.0.1:12345"},
	}

	mgr, err := NewUpstreamManager(nil)
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	router, err := NewRouter(RoutingConfig{
		Final: "direct",
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	// HandleTProxy 在 redirect 模式下应因 originalDst 失败而返回
	done := make(chan struct{})
	go func() {
		defer close(done)
		HandleTProxy(conn, mgr, router, "redirect")
	}()

	select {
	case <-done:
		// 预期行为：originalDst 失败后 HandleTProxy 返回
	case <-time.After(5 * time.Second):
		t.Fatal("HandleTProxy did not return within 5 seconds")
	}

	if !conn.closed {
		t.Error("connection should be closed after HandleTProxy returns")
	}
}

func TestHandleTProxyTProxyModeEndToEnd(t *testing.T) {
	// 启动一个目标 TCP 服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	targetAddr := ln.Addr().String()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		// 读取数据并回显
		buf := make([]byte, 1024)
		n, _ := c.Read(buf)
		if n > 0 {
			c.Write(buf[:n])
		}
	}()

	// 创建一对真实 TCP 连接模拟 TPROXY 连接
	// 使用 net.Pipe 但设置短超时
	SetTimeouts(TimeoutConfig{
		TunnelIdle: Duration(500 * time.Millisecond),
	})

	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()

	// 包装 serverSide 以覆盖 LocalAddr
	serverConn := &pipeConnWrapper{
		Conn:       serverSide,
		localAddr:  &mockAddr{addr: targetAddr},
		remoteAddr: &mockAddr{addr: "10.0.0.1:12345"},
	}

	mgr, err := NewUpstreamManager(nil)
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	router, err := NewRouter(RoutingConfig{
		Final: "direct",
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Stop()

	// 在 goroutine 中处理 TPROXY 连接
	done := make(chan struct{})
	go func() {
		defer close(done)
		HandleTProxy(serverConn, mgr, router, "tproxy")
	}()

	// 发送测试数据
	testData := "hello tproxy"
	go func() {
		clientSide.Write([]byte(testData))
	}()

	// 读取回显的响应
	buf := make([]byte, 1024)
	clientSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := clientSide.Read(buf)
	if err != nil {
		t.Logf("clientSide.Read: %v", err)
	} else if string(buf[:n]) != testData {
		t.Errorf("response got=%q, want=%q", string(buf[:n]), testData)
	}

	// 关闭客户端以触发隧道关闭
	clientSide.Close()

	select {
	case <-done:
		// 预期行为
	case <-time.After(5 * time.Second):
		t.Fatal("HandleTProxy did not return within 5 seconds")
	}
}

// pipeConnWrapper 包装 net.Pipe 的连接端，覆盖 LocalAddr/RemoteAddr。
type pipeConnWrapper struct {
	net.Conn
	localAddr  net.Addr
	remoteAddr net.Addr
}

func (c *pipeConnWrapper) LocalAddr() net.Addr  { return c.localAddr }
func (c *pipeConnWrapper) RemoteAddr() net.Addr { return c.remoteAddr }

func TestIsTProxySupported(t *testing.T) {
	// 在 Linux 上应返回 true，其他平台返回 false
	// 这里只验证函数可调用且不 panic
	_ = IsTProxySupported()
}
