package proxy

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRouterDecideUsesFirstMatchingRule(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Rules: []RouteRule{
			{Type: "domain_suffix", Values: []string{"github.com"}, Action: ActionProxy, Upstreams: []string{"hk", "sg"}},
			{Type: "domain_keyword", Values: []string{"internal"}, Action: ActionDirect},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	decision := r.Decide("api.github.com:443")
	if decision.Action != ActionProxy {
		t.Fatalf("github action = %q, want %q", decision.Action, ActionProxy)
	}
	if len(decision.Upstreams) != 2 || decision.Upstreams[0] != "hk" || decision.Upstreams[1] != "sg" {
		t.Fatalf("github upstreams = %#v, want [hk sg]", decision.Upstreams)
	}

	decision = r.Decide("service.internal.example:80")
	if decision.Action != ActionDirect {
		t.Fatalf("internal action = %q, want %q", decision.Action, ActionDirect)
	}

	decision = r.Decide("example.net:80")
	if decision.Action != ActionAuto {
		t.Fatalf("final action = %q, want %q", decision.Action, ActionAuto)
	}
}

func TestRouterDecideIPAndRegex(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionDirect,
		Rules: []RouteRule{
			{Type: "ip_cidr", Values: []string{"10.0.0.0/8"}, Action: ActionProxy},
			{Type: "domain_regex", Values: []string{`.*\.cn$`}, Action: ActionDirect},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Decide("10.0.0.1:443"); got.Action != ActionProxy {
		t.Fatalf("expected proxy for 10.0.0.1, got %s", got.Action)
	}
	if got := r.Decide("news.cn:80"); got.Action != ActionDirect {
		t.Fatalf("expected direct for .cn, got %s", got.Action)
	}
	if got := r.Decide("other.com:443"); got.Action != ActionDirect {
		t.Fatalf("expected final direct, got %s", got.Action)
	}
}

func TestRouterNilDecide(t *testing.T) {
	var r *Router
	if got := r.Decide("example.com:80"); got.Action != ActionAuto {
		t.Fatalf("expected auto for nil router, got %s", got.Action)
	}
}

func TestCompileRouteRuleErrors(t *testing.T) {
	if _, err := compileRouteRule(RouteRule{Type: ""}); err == nil {
		t.Fatal("expected error for empty type")
	}
	if _, err := compileRouteRule(RouteRule{Type: "ip_cidr", Values: []string{"invalid"}}); err == nil {
		t.Fatal("expected error for invalid cidr")
	}
	if _, err := compileRouteRule(RouteRule{Type: "domain_regex", Values: []string{"("}}); err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if _, err := compileRouteRule(RouteRule{Type: "unknown"}); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestNormalizeHelpers(t *testing.T) {
	if normalizeRuleType("domain-keyword") != "domain_keyword" {
		t.Fatal("normalizeRuleType failed")
	}
	if normalizeAction(" PROXY ") != "proxy" {
		t.Fatal("normalizeAction failed")
	}
	if normalizeAutoMode("proxy_first") != "proxy_first" {
		t.Fatal("normalizeAutoMode failed")
	}
	if normalizeAutoMode("direct_first") != "direct_first" {
		t.Fatal("normalizeAutoMode default failed")
	}
	if normalizeDomainRuleValue("+.Example.COM.") != "example.com" {
		t.Fatalf("normalizeDomainRuleValue failed: %q", normalizeDomainRuleValue("+.Example.COM."))
	}
	if normalizeGeoSiteCode(" geosite:cn ") != "CN" {
		t.Fatalf("normalizeGeoSiteCode failed: %q", normalizeGeoSiteCode(" geosite:cn "))
	}
	if !validAction("direct") || !validAction("proxy") || !validAction("auto") || validAction("x") {
		t.Fatal("validAction failed")
	}
	if !validAction(ActionDirectFirst) || !validAction(ActionProxyFirst) {
		t.Fatal("validAction should accept prioritized actions")
	}
	if normalizeAction("DIRECT-FIRST") != ActionDirectFirst {
		t.Fatalf("normalizeAction hyphen failed: %q", normalizeAction("DIRECT-FIRST"))
	}
}

// 禁用的规则必须从匹配链里消失，否则"临时停用"没有意义。
func TestDisabledRuleSkipped(t *testing.T) {
	router, err := NewRouter(RoutingConfig{
		Final: ActionDirect,
		Rules: []RouteRule{
			{Type: "domain_suffix", Values: []string{"example.com"}, Action: ActionProxy, Disabled: true},
			{Type: "domain_keyword", Values: []string{"other"}, Action: ActionDeny},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := router.Decide("www.example.com"); got.Action != ActionDirect {
		t.Fatalf("disabled rule should be skipped, got %+v", got)
	}
	if got := router.Decide("other.example.org"); got.Action != ActionDeny {
		t.Fatalf("enabled rule after disabled one should still match, got %+v", got)
	}
}

func TestCompilePrioritizedActionMode(t *testing.T) {
	compiled, err := compileRouteRule(RouteRule{Type: "match", Action: "proxy-first"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.decision.Action != ActionProxyFirst || compiled.decision.Mode != "proxy_first" {
		t.Fatalf("unexpected decision: %+v", compiled.decision)
	}

	compiled, err = compileRouteRule(RouteRule{Type: "match", Action: ActionAuto})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.decision.Mode != "" {
		t.Fatalf("auto should keep global mode, got %q", compiled.decision.Mode)
	}
}

func TestRouterDialDirect(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	target := startMockTarget(t)
	conn, err := router.Dial(target, nil)
	if err != nil {
		t.Fatalf("dial direct: %v", err)
	}
	defer conn.Close()
	conn.Write([]byte("hello"))
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	if string(buf[:n]) != "hello" {
		t.Fatalf("unexpected response %q", buf[:n])
	}
}

func TestRouterDialProxy(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{
		Final: ActionDirect,
		Rules: []RouteRule{{Type: "domain", Values: []string{"target.local"}, Action: ActionProxy, Upstreams: []string{"up"}}},
	}, DNSConfig{})
	proxyAddr := startMockSocks5Proxy(t)
	mgr, _ := NewUpstreamManager([]UpstreamConfig{{Name: "up", URL: "socks5://" + proxyAddr}})
	target := startMockTarget(t)
	conn, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.Write([]byte("hello"))
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	if string(buf[:n]) != "hello" {
		t.Fatalf("unexpected response %q", buf[:n])
	}
}

func TestRouterDialAutoDirect(t *testing.T) {
	router, _ := NewRouter(RoutingConfig{Final: ActionAuto}, DNSConfig{})
	target := startMockTarget(t)
	conn, err := router.Dial(target, nil)
	if err != nil {
		t.Fatalf("dial auto: %v", err)
	}
	defer conn.Close()
}

func TestRouterDialAutoConcurrentConnectionsAreIndependent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const clients = 8
	var serverWG sync.WaitGroup
	serverWG.Add(clients + 1) // one probe connection plus one connection per caller
	go func() {
		for i := 0; i < clients+1; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer serverWG.Done()
				defer c.Close()
				buf := make([]byte, 32)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(buf[:n])
			}(conn)
		}
	}()

	router, err := NewRouter(RoutingConfig{Final: ActionAuto}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, clients)
	var clientWG sync.WaitGroup
	for i := 0; i < clients; i++ {
		clientWG.Add(1)
		go func(id int) {
			defer clientWG.Done()
			<-start
			conn, err := router.Dial(ln.Addr().String(), nil)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			message := fmt.Sprintf("client-%d", id)
			if _, err := conn.Write([]byte(message)); err != nil {
				errs <- err
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, len(message))
			if _, err := conn.Read(buf); err != nil {
				errs <- err
				return
			}
			if string(buf) != message {
				errs <- fmt.Errorf("received %q, want %q", buf, message)
			}
		}(i)
	}
	close(start)
	clientWG.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	ln.Close()
	serverWG.Wait()
}

func TestSocks5HRedirectConfig(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"hk", "sg"}},
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	if !r.Socks5HEnabled() {
		t.Fatal("socks5h redirect should be enabled")
	}
	if len(r.socks5h.Upstreams) != 2 || r.socks5h.Upstreams[0] != "hk" || r.socks5h.Upstreams[1] != "sg" {
		t.Fatalf("socks5h upstreams = %#v, want [hk sg]", r.socks5h.Upstreams)
	}
}

func TestNormalizeGeoSiteConfig(t *testing.T) {
	cfg := GeoSiteConfig{Path: "geosite.dat"}
	NormalizeGeoSiteConfig(&cfg)
	if cfg.UpdateURL == "" {
		t.Fatal("default update url was not set")
	}
	if cfg.UpdateInterval == 0 {
		t.Fatal("default update interval was not set")
	}
}
