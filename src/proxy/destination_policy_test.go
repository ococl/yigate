package proxy

import (
	"bufio"
	"net"
	"testing"
)

func newTestPolicy(t *testing.T, cfg DestinationPolicyConfig) *DestinationPolicy {
	t.Helper()
	p, err := NewDestinationPolicy(cfg)
	if err != nil {
		t.Fatalf("NewDestinationPolicy: %v", err)
	}
	return p
}

func TestDestinationPolicyProtectsPrivateByDefault(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	cases := []string{
		"127.0.0.1:80",
		"10.0.0.5:443",
		"192.168.1.1:8080",
		"169.254.169.254:80", // 云元数据
		"172.16.0.1:22",
		"0.0.0.0:1",
		"[::1]:80",
		"[fc00::1]:80",
		"[fe80::1]:80",
		"[ff02::1]:80",
		"224.0.0.1:1",
	}
	for _, c := range cases {
		if err := p.CheckTarget(c); err == nil {
			t.Fatalf("expected %q to be denied by default", c)
		}
	}
}

func TestDestinationPolicyAllowsPublicByDefault(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	if err := p.CheckTarget("93.184.216.34:443"); err != nil {
		t.Fatalf("public target denied: %v", err)
	}
}

func TestDestinationPolicyExplicitDenyAndAllow(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{
		ProtectPrivate: false,
		DefaultAction:  ActionDeny,
		AllowCIDRs:     []string{"10.0.0.0/8"},
		AllowDomains:   []string{"example.com"},
	})
	if err := p.CheckTarget("10.1.2.3:443"); err != nil {
		t.Fatalf("allowed cidr denied: %v", err)
	}
	if err := p.CheckTarget("8.8.8.8:443"); err == nil {
		t.Fatal("non-allowlisted ip should be denied")
	}
	if err := p.CheckTarget("api.example.com:443"); err != nil {
		t.Fatalf("allowed domain denied: %v", err)
	}
	if err := p.CheckTarget("evil.other.com:443"); err == nil {
		t.Fatal("non-allowlisted domain should be denied")
	}
}

func TestDestinationPolicyDenyPortsAndDomains(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{
		ProtectPrivate: false,
		DefaultAction:  ActionAllow,
		DenyPorts:      []int{25},
		DenyDomains:    []string{"internal.local"},
	})
	if err := p.CheckTarget("1.2.3.4:25"); err == nil {
		t.Fatal("denied port should be blocked")
	}
	if err := p.CheckTarget("host.internal.local:443"); err == nil {
		t.Fatal("denied domain should be blocked")
	}
	if err := p.CheckTarget("1.2.3.4:443"); err != nil {
		t.Fatalf("allowed target wrongly denied: %v", err)
	}
}

func TestDestinationPolicyRejectsBadConfig(t *testing.T) {
	if _, err := NewDestinationPolicy(DestinationPolicyConfig{DefaultAction: "maybe"}); err == nil {
		t.Fatal("invalid default_action should be rejected")
	}
	if _, err := NewDestinationPolicy(DestinationPolicyConfig{DenyCIDRs: []string{"not-a-cidr"}}); err == nil {
		t.Fatal("invalid cidr should be rejected")
	}
}

func TestRouterDenyRuleBlocksDial(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Rules: []RouteRule{
			{Type: "ip_cidr", Values: []string{"93.184.216.34/32"}, Action: ActionDeny},
		},
	}, DNSConfig{})
	if got := router.Decide("93.184.216.34:443").Action; got != ActionDeny {
		t.Fatalf("deny rule not applied, got %q", got)
	}
	if _, err := router.Dial("93.184.216.34:443", nil); err == nil {
		t.Fatal("dial should be denied by route rule")
	}
}

func TestRouterMatchDenyRejected(t *testing.T) {
	if _, err := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Rules: []RouteRule{{Type: "match", Values: nil, Action: ActionDeny}},
	}, DNSConfig{}); err == nil {
		t.Fatal("match+deny should be rejected to avoid global block")
	}
}

func TestDialDirectCheckedBlocksProtectedIP(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{
		Final:  ActionDirect,
		Policy: newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow}),
	}, DNSConfig{})
	if _, err := router.Dial("127.0.0.1:80", nil); err == nil {
		t.Fatal("loopback dial should be denied by policy")
	}
	// 合法公网地址在没有可达目标时会因连接失败而非策略拒绝，这里只验证策略不误拦。
}

func TestUDPRelayPolicyBlocksTarget(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	relay := &udpRelay{policy: p, sessions: make(map[string]*udpSession), quit: make(chan struct{})}
	err := relay.forwardToTarget("127.0.0.1:53", []byte("ping"))
	if err == nil {
		t.Fatal("udp relay to loopback should be denied by policy")
	}
}

func TestSOCKS5BindDisabledByDefault(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		br := bufio.NewReader(conn)
		HandleSOCKS5(conn, br, nil, nil, nil, nil, false, false)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// auth + BIND request to a dummy target
	conn.Write([]byte{0x05, 0x01, 0x00})
	buf := make([]byte, 2)
	if _, err := conn.Read(buf); err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x50})
	rep := make([]byte, 10)
	if _, err := conn.Read(rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != 0x07 {
		t.Fatalf("expected command-not-supported (0x07), got 0x%02x", rep[1])
	}
}
