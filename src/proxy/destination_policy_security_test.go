package proxy

import (
	"bufio"
	"net"
	"net/http"
	"testing"
)

// 严格模式下的白名单语义：deny_* 与 protect_private 永远优先，
// allow_ports 是附加约束，必须与地址/域名白名单同时满足。
func TestDestinationPolicyDenyWinsOverAllowPorts(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{
		ProtectPrivate: true,
		DefaultAction:  ActionDeny,
		AllowPorts:     []int{443},
		AllowDomains:   []string{"good.com"},
		DenyDomains:    []string{"evil.com"},
	})
	allowed := []string{"good.com:443", "api.good.com:443"}
	for _, target := range allowed {
		if err := p.CheckTarget(target); err != nil {
			t.Fatalf("%q should be allowed: %v", target, err)
		}
	}
	denied := []string{
		"evil.com:443",     // deny_domains 不能被 allow_ports 覆盖
		"good.com:80",      // 端口不在白名单
		"8.8.8.8:443",      // 地址不在白名单
		"127.0.0.1:443",    // 受保护地址
		"169.254.169.254:443", // 云元数据
	}
	for _, target := range denied {
		if err := p.CheckTarget(target); err == nil {
			t.Fatalf("%q should be denied", target)
		}
	}
}

// 调用方已提供解析后的 IP 时，域名白名单仍必须参与判断。
func TestDestinationPolicyAllowDomainsWithResolvedIP(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{
		DefaultAction: ActionDeny,
		AllowDomains:  []string{"good.com"},
	})
	if err := p.Check("api.good.com", 443, net.ParseIP("8.8.8.8")); err != nil {
		t.Fatalf("allow_domains should apply even when ip is resolved: %v", err)
	}
	if err := p.Check("api.other.com", 443, net.ParseIP("8.8.8.8")); err == nil {
		t.Fatal("non-allowlisted domain should be denied")
	}
}

// net.ParseIP 不接受、但系统解析器会还原为 IPv4 的字面量必须按 IP 校验，
// 否则会被当成域名放行，形成 SSRF 绕过。
func TestDestinationPolicyBlocksNonStandardIPLiterals(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	for _, target := range []string{
		"2130706433:80",
		"0x7f000001:80",
		"0177.0.0.1:80",
		"127.1:80",
		"0x0a000001:80",
	} {
		if err := p.CheckTarget(target); err == nil {
			t.Fatalf("non-standard loopback literal %q should be denied", target)
		}
	}
	if err := p.CheckTarget("example.com:80"); err != nil {
		t.Fatalf("ordinary domain should not be affected: %v", err)
	}
}

func TestDestinationPolicyBlocksTransitionAndReservedRanges(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	for _, target := range []string{
		"100.64.0.1:80",
		"192.0.0.1:80",
		"198.18.0.1:80",
		"240.0.0.1:80",
		"[2002:7f00:1::]:80",   // 6to4
		"[2001::7f00:1]:80",     // Teredo
		"[64:ff9b::7f00:1]:80", // NAT64
		"[2001:db8::1]:80",      // 文档地址
	} {
		if err := p.CheckTarget(target); err == nil {
			t.Fatalf("protected range %q should be denied", target)
		}
	}
}

// 无端口输入不再静默放行，域名 deny 仍应生效。
func TestDestinationPolicyCheckTargetWithoutPort(t *testing.T) {
	p := newTestPolicy(t, DestinationPolicyConfig{
		ProtectPrivate: true,
		DefaultAction:  ActionAllow,
		DenyDomains:    []string{"internal.local"},
	})
	if err := p.CheckTarget("host.internal.local"); err == nil {
		t.Fatal("denied domain without port should not be silently allowed")
	}
	if err := p.CheckTarget("127.0.0.1"); err == nil {
		t.Fatal("non-standard bare IP should still be parsed for policy checks")
	}
}

func TestDestinationPolicyRejectsAllowPortsWithoutAddressAllowlist(t *testing.T) {
	_, err := NewDestinationPolicy(DestinationPolicyConfig{
		DefaultAction: ActionDeny,
		AllowPorts:    []int{443},
	})
	if err == nil {
		t.Fatal("allow_ports alone under default_action=deny should be rejected at load time")
	}
}

// 普通 HTTP 请求必须在进入 proxy / auto / socks5h 分支之前被策略拦截。
// mgr 传 nil：若检查缺失，该用例会直接 panic 而不是得到 403。
func TestHandleHTTPRequestAppliesPolicyBeforeProxy(t *testing.T) {
	policy := newTestPolicy(t, DestinationPolicyConfig{
		DefaultAction: ActionAllow,
		DenyDomains:   []string{"internal.local"},
	})
	router, err := NewRouter(RoutingConfig{
		Final:  ActionProxy,
		Policy: policy,
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, "http://host.internal.local/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "host.internal.local"
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleHTTPRequest(server, req, nil, router, "", "")
	}()

	resp, err := http.ReadResponse(bufio.NewReader(client), req)
	if err != nil {
		t.Fatalf("read proxy response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 before proxying, got %d", resp.StatusCode)
	}
	<-done
}

func TestDialSocks5HAppliesPolicy(t *testing.T) {
	policy := newTestPolicy(t, DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	router, err := NewRouter(RoutingConfig{
		Final:           ActionProxy,
		Policy:          policy,
		Socks5HRedirect: Socks5HConfig{Enabled: true},
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	if _, err := router.DialSocks5H("127.0.0.1:80", nil); err == nil {
		t.Fatal("socks5h dial to loopback should be denied by policy")
	}
}
