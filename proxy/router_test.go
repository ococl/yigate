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

func TestGeoSiteCodeNormalization(t *testing.T) {
	codes := geositeCodes([]RouteRule{
		{Type: "geosite", Values: []string{"GEOSITE:CN", "google", " cn "}},
		{Type: "domain_suffix", Values: []string{"example.com"}},
	})
	want := []string{"CN", "GOOGLE"}
	if len(codes) != len(want) {
		t.Fatalf("codes = %#v, want %#v", codes, want)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %#v, want %#v", codes, want)
		}
	}
}
