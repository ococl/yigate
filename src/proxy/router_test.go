package proxy

import "testing"

func TestRouterDecideUsesFirstMatchingRule(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionAuto,
		Rules: []RouteRule{
			{Type: "domain_suffix", Values: []string{"github.com"}, Action: ActionProxy, Upstreams: []string{"hk", "sg"}},
			{Type: "domain_keyword", Values: []string{"internal"}, Action: ActionDirect},
		},
	})
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

func TestSocks5HRedirectConfig(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"hk", "sg"}},
	})
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
