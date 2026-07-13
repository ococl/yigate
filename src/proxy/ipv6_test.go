package proxy

import (
	"bufio"
	"bytes"
	"net"
	"testing"
)

func TestSortAddrsByPrecedence_IPv4First(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("2001:db8::1")},
		{IP: net.ParseIP("127.0.0.1")},
		{IP: net.ParseIP("::1")},
		{IP: net.ParseIP("192.168.1.1")},
	}
	got := sortAddrsByPrecedence(addrs, IPPrecedenceIPv4First)
	if len(got) != 4 {
		t.Fatalf("expected 4 addresses, got %d", len(got))
	}
	// IPv4 should come first
	if got[0].To4() == nil {
		t.Fatalf("expected IPv4 first, got %s", got[0])
	}
	if got[1].To4() == nil {
		t.Fatalf("expected IPv4 second, got %s", got[1])
	}
	// IPv6 should come after
	if got[2].To4() != nil {
		t.Fatalf("expected IPv6 third, got %s", got[2])
	}
	if got[3].To4() != nil {
		t.Fatalf("expected IPv6 fourth, got %s", got[3])
	}
}

func TestSortAddrsByPrecedence_IPv6First(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("127.0.0.1")},
		{IP: net.ParseIP("2001:db8::1")},
		{IP: net.ParseIP("192.168.1.1")},
		{IP: net.ParseIP("::1")},
	}
	got := sortAddrsByPrecedence(addrs, IPPrecedenceIPv6First)
	if len(got) != 4 {
		t.Fatalf("expected 4 addresses, got %d", len(got))
	}
	// IPv6 should come first
	if got[0].To4() != nil {
		t.Fatalf("expected IPv6 first, got %s", got[0])
	}
	if got[1].To4() != nil {
		t.Fatalf("expected IPv6 second, got %s", got[1])
	}
	// IPv4 should come after
	if got[2].To4() == nil {
		t.Fatalf("expected IPv4 third, got %s", got[2])
	}
	if got[3].To4() == nil {
		t.Fatalf("expected IPv4 fourth, got %s", got[3])
	}
}

func TestSortAddrsByPrecedence_System(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("2001:db8::1")},
		{IP: net.ParseIP("127.0.0.1")},
		{IP: net.ParseIP("::1")},
	}
	got := sortAddrsByPrecedence(addrs, IPPrecedenceSystem)
	if len(got) != 3 {
		t.Fatalf("expected 3 addresses, got %d", len(got))
	}
	// System order preserves DNS response order
	if !got[0].Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("expected first address to be 2001:db8::1, got %s", got[0])
	}
	if !got[1].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("expected second address to be 127.0.0.1, got %s", got[1])
	}
}

func TestSortAddrsByPrecedence_Empty(t *testing.T) {
	got := sortAddrsByPrecedence(nil, IPPrecedenceIPv4First)
	if len(got) != 0 {
		t.Fatalf("expected 0 addresses, got %d", len(got))
	}
}

func TestSortAddrsByPrecedence_IPv4Only(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("10.0.0.1")},
		{IP: net.ParseIP("192.168.1.1")},
	}
	for _, prec := range []string{IPPrecedenceIPv4First, IPPrecedenceIPv6First, IPPrecedenceSystem} {
		got := sortAddrsByPrecedence(addrs, prec)
		if len(got) != 2 {
			t.Fatalf("precedence=%s: expected 2 addresses, got %d", prec, len(got))
		}
		if got[0].To4() == nil {
			t.Fatalf("precedence=%s: expected IPv4, got %s", prec, got[0])
		}
	}
}

func TestSortAddrsByPrecedence_IPv6Only(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("2001:db8::1")},
		{IP: net.ParseIP("::1")},
	}
	for _, prec := range []string{IPPrecedenceIPv4First, IPPrecedenceIPv6First, IPPrecedenceSystem} {
		got := sortAddrsByPrecedence(addrs, prec)
		if len(got) != 2 {
			t.Fatalf("precedence=%s: expected 2 addresses, got %d", prec, len(got))
		}
		if got[0].To4() != nil {
			t.Fatalf("precedence=%s: expected IPv6, got %s", prec, got[0])
		}
	}
}

func TestNormalizeIPPrecedence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", IPPrecedenceIPv4First},
		{"ipv4_first", IPPrecedenceIPv4First},
		{"IPv4_First", IPPrecedenceIPv4First},
		{"ipv6_first", IPPrecedenceIPv6First},
		{"system", IPPrecedenceSystem},
		{"SYSTEM", IPPrecedenceSystem},
		{"unknown", IPPrecedenceIPv4First},
		{"  ipv6_first  ", IPPrecedenceIPv6First},
	}
	for _, tt := range tests {
		got := normalizeIPPrecedence(tt.input)
		if got != tt.want {
			t.Errorf("normalizeIPPrecedence(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRouterIPPrecedence(t *testing.T) {
	// Default: ipv4_first
	r, err := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.IPPrecedence() != IPPrecedenceIPv4First {
		t.Fatalf("expected default ipv4_first, got %s", r.IPPrecedence())
	}

	// Configured: ipv6_first
	r2, err := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{IPPrecedence: "ipv6_first"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.IPPrecedence() != IPPrecedenceIPv6First {
		t.Fatalf("expected ipv6_first, got %s", r2.IPPrecedence())
	}

	// Configured: system
	r3, err := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{IPPrecedence: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if r3.IPPrecedence() != IPPrecedenceSystem {
		t.Fatalf("expected system, got %s", r3.IPPrecedence())
	}

	// Nil router
	var rNil *Router
	if rNil.IPPrecedence() != IPPrecedenceIPv4First {
		t.Fatalf("expected default ipv4_first for nil router, got %s", rNil.IPPrecedence())
	}
}

func TestValidateDNSConfig_IPPrecedence(t *testing.T) {
	tests := []struct {
		prec string
		ok   bool
	}{
		{"", true},
		{"ipv4_first", true},
		{"ipv6_first", true},
		{"system", true},
		{"unknown", false},
		{"ipv4", false},
	}
	for _, tt := range tests {
		err := ValidateDNSConfig(DNSConfig{Type: "system", IPPrecedence: tt.prec})
		if tt.ok && err != nil {
			t.Errorf("ValidateDNSConfig(prec=%q) unexpected error: %v", tt.prec, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateDNSConfig(prec=%q) expected error, got nil", tt.prec)
		}
	}
}

// TestIPv6CIDRRouteRule 测试路由规则支持 IPv6 CIDR。
func TestIPv6CIDRRouteRule(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionDirect,
		Rules: []RouteRule{
			{
				Type:   "ip_cidr",
				Values: []string{"2001:db8::/32", "::1/128"},
				Action: ActionProxy,
			},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// IPv6 address in CIDR should match
	d := r.Decide("[2001:db8::1]:443")
	if d.Action != ActionProxy {
		t.Fatalf("expected proxy for 2001:db8::1, got %s", d.Action)
	}

	// IPv6 loopback should match
	d = r.Decide("[::1]:443")
	if d.Action != ActionProxy {
		t.Fatalf("expected proxy for ::1, got %s", d.Action)
	}

	// IPv6 address outside CIDR should fall through to final
	d = r.Decide("[2001:db9::1]:443")
	if d.Action != ActionDirect {
		t.Fatalf("expected direct for 2001:db9::1, got %s", d.Action)
	}

	// IPv4 address should not match IPv6 CIDR rule
	d = r.Decide("192.168.1.1:443")
	if d.Action != ActionDirect {
		t.Fatalf("expected direct for IPv4, got %s", d.Action)
	}
}

// TestIPv6DestinationPolicy 测试目标策略支持 IPv6 地址。
func TestIPv6DestinationPolicy(t *testing.T) {
	p, err := NewDestinationPolicy(DestinationPolicyConfig{
		ProtectPrivate: true,
		DenyCIDRs:      []string{"2001:db8::/32"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// IPv6 in deny list
	err = p.Check("2001:db8::1", 443, net.ParseIP("2001:db8::1"))
	if err == nil {
		t.Fatal("expected error for denied IPv6 address")
	}

	// IPv6 loopback is protected
	err = p.Check("::1", 443, net.ParseIP("::1"))
	if err == nil {
		t.Fatal("expected error for protected IPv6 loopback")
	}

	// IPv6 ULA is protected
	err = p.Check("fc00::1", 443, net.ParseIP("fc00::1"))
	if err == nil {
		t.Fatal("expected error for protected IPv6 ULA")
	}

	// IPv6 link-local is protected
	err = p.Check("fe80::1", 443, net.ParseIP("fe80::1"))
	if err == nil {
		t.Fatal("expected error for protected IPv6 link-local")
	}

	// IPv6 multicast is protected
	err = p.Check("ff02::1", 443, net.ParseIP("ff02::1"))
	if err == nil {
		t.Fatal("expected error for protected IPv6 multicast")
	}

	// IPv6 unspecified is protected
	err = p.Check("::", 443, net.ParseIP("::"))
	if err == nil {
		t.Fatal("expected error for protected IPv6 unspecified")
	}

	// Public IPv6 should be allowed
	err = p.Check("2606:4700::1", 443, net.ParseIP("2606:4700::1"))
	if err != nil {
		t.Fatalf("expected no error for public IPv6, got %v", err)
	}
}

// TestIPv6DestinationPolicyCheckTarget 测试 CheckTarget 处理 IPv6 目标。
func TestIPv6DestinationPolicyCheckTarget(t *testing.T) {
	p, err := NewDestinationPolicy(DestinationPolicyConfig{
		ProtectPrivate: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// IPv6 with brackets and port
	err = p.CheckTarget("[::1]:443")
	if err == nil {
		t.Fatal("expected error for IPv6 loopback with port")
	}

	// IPv6 without brackets (bare)
	err = p.CheckTarget("::1")
	if err == nil {
		t.Fatal("expected error for bare IPv6 loopback")
	}

	// Public IPv6 with brackets and port
	err = p.CheckTarget("[2606:4700::1]:443")
	if err != nil {
		t.Fatalf("expected no error for public IPv6, got %v", err)
	}
}

// TestIPv6AccessControl 测试访问控制支持 IPv6 CIDR。
func TestIPv6AccessControl(t *testing.T) {
	ac, err := NewAccessController(AccessControlConfig{
		AllowedClients: []string{"::1/128", "2001:db8::/32"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// IPv6 loopback should be allowed
	if !ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("::1"), Port: 12345}) {
		t.Fatal("expected ::1 to be allowed")
	}

	// IPv6 in allowed range
	if !ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 12345}) {
		t.Fatal("expected 2001:db8::1 to be allowed")
	}

	// IPv6 outside allowed range
	if ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("2606:4700::1"), Port: 12345}) {
		t.Fatal("expected 2606:4700::1 to be denied")
	}

	// IPv4 should not match IPv6 CIDR
	if ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}) {
		t.Fatal("expected 127.0.0.1 to be denied (not in IPv6 allow list)")
	}
}

// TestIPv6NormalizeTargetHost 测试 normalizeTargetHost 处理 IPv6 地址。
func TestIPv6NormalizeTargetHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"[::1]:443", "::1"},
		{"[2001:db8::1]:80", "2001:db8::1"},
		{"[::1]", "::1"},
		{"::1", "::1"},
		{"[FE80::1]:443", "fe80::1"},
		{"192.168.1.1:443", "192.168.1.1"},
		{"example.com:443", "example.com"},
	}
	for _, tt := range tests {
		got := normalizeTargetHost(tt.input)
		if got != tt.want {
			t.Errorf("normalizeTargetHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestIPv6SOCKS5ReadRequest 测试 SOCKS5 解析 IPv6 地址类型 (0x04)。
func TestIPv6SOCKS5ReadRequest(t *testing.T) {
	// 构造 SOCKS5 CONNECT 请求，IPv6 地址类型
	// VER=5, CMD=1, RSV=0, ATYP=4, IPv6=2001:db8::1 (16 bytes), PORT=443 (0x01BB)
	req := []byte{
		0x05, 0x01, 0x00, 0x04,
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x01, 0xBB,
	}
	r := bufio.NewReader(bytes.NewReader(req))
	cmd, target, rep, err := socks5ReadRequest(r)
	if err != nil {
		t.Fatalf("socks5ReadRequest failed: %v", err)
	}
	if cmd != 0x01 {
		t.Fatalf("expected cmd=1, got %d", cmd)
	}
	if rep != 0x00 {
		t.Fatalf("expected rep=0, got %d", rep)
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}
	if net.ParseIP(host) == nil || net.ParseIP(host).To4() != nil {
		t.Fatalf("expected IPv6 host, got %s", host)
	}
	if port != "443" {
		t.Fatalf("expected port 443, got %s", port)
	}
}

// TestIPv6SOCKS5Reply 测试 SOCKS5 回复支持 IPv6 地址类型。
func TestIPv6SOCKS5Reply(t *testing.T) {
	// 使用 IPv6 地址回复
	addr := "[2001:db8::1]:443"
	client, server := net.Pipe()
	go func() {
		_ = socks5ReplyWithAddr(client, 0x00, addr)
		client.Close()
	}()
	buf := make([]byte, 32)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	server.Close()

	reply := buf[:n]
	if len(reply) < 4 {
		t.Fatalf("reply too short: %d bytes", len(reply))
	}
	if reply[0] != 0x05 {
		t.Fatalf("expected version 5, got %d", reply[0])
	}
	if reply[3] != 0x04 {
		t.Fatalf("expected IPv6 address type 0x04, got 0x%02x", reply[3])
	}
	if len(reply) != 4+16+2 {
		t.Fatalf("expected 22 bytes for IPv6 reply, got %d", len(reply))
	}
}

// TestIPv6SOCKS5ReplyUnspecified 测试 SOCKS5 回复将 :: 替换为 127.0.0.1。
func TestIPv6SOCKS5ReplyUnspecified(t *testing.T) {
	addr := "[::]:0"
	client, server := net.Pipe()
	go func() {
		_ = socks5ReplyWithAddr(client, 0x00, addr)
		client.Close()
	}()
	buf := make([]byte, 32)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	server.Close()

	reply := buf[:n]
	// :: should be replaced with 127.0.0.1, so address type should be 0x01 (IPv4)
	if reply[3] != 0x01 {
		t.Fatalf("expected IPv4 address type 0x01 for unspecified, got 0x%02x", reply[3])
	}
}

// TestIPv6ProtectedRanges 测试受保护的 IPv6 地址段。
func TestIPv6ProtectedRanges(t *testing.T) {
	protected := []string{
		"::1",      // loopback
		"::",       // unspecified
		"fc00::1",  // ULA
		"fd00::1",  // ULA
		"fe80::1",  // link-local
		"ff00::1",  // multicast
		"ff02::1",  // multicast
	}
	for _, ip := range protected {
		if !isProtectedIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to be protected", ip)
		}
	}

	notProtected := []string{
		"2606:4700::1",   // public
		"2001:4860:4860::8888", // Google DNS
	}
	for _, ip := range notProtected {
		if isProtectedIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to NOT be protected", ip)
		}
	}
}

// TestIPv6MixedCIDRRouteRule 测试混合 IPv4/IPv6 CIDR 路由规则。
func TestIPv6MixedCIDRRouteRule(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionDirect,
		Rules: []RouteRule{
			{
				Type:   "ip_cidr",
				Values: []string{"10.0.0.0/8", "fc00::/7"},
				Action: ActionProxy,
			},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// IPv4 in CIDR
	d := r.Decide("10.1.2.3:443")
	if d.Action != ActionProxy {
		t.Fatalf("expected proxy for 10.1.2.3, got %s", d.Action)
	}

	// IPv6 in CIDR
	d = r.Decide("[fc00::1]:443")
	if d.Action != ActionProxy {
		t.Fatalf("expected proxy for fc00::1, got %s", d.Action)
	}

	// Public IPv4
	d = r.Decide("8.8.8.8:443")
	if d.Action != ActionDirect {
		t.Fatalf("expected direct for 8.8.8.8, got %s", d.Action)
	}

	// Public IPv6
	d = r.Decide("[2606:4700::1]:443")
	if d.Action != ActionDirect {
		t.Fatalf("expected direct for 2606:4700::1, got %s", d.Action)
	}
}
