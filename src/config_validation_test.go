package main

import (
	"strings"
	"testing"

	"yigate/proxy"
)

func TestValidateRouteRuleRejectsUnknownType(t *testing.T) {
	err := validateRouteRule(proxy.RouteRule{Type: "bogus", Values: []string{"x"}, Action: "proxy"}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported rule type")
	}
}

func TestValidateRouteRuleValidatesRegexAndCIDR(t *testing.T) {
	if err := validateRouteRule(proxy.RouteRule{
		Type: "domain_regex", Values: []string{"("}, Action: "direct",
	}, nil); err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if err := validateRouteRule(proxy.RouteRule{
		Type: "ip_cidr", Values: []string{"10.0.0.0/33"}, Action: "direct",
	}, nil); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if err := validateRouteRule(proxy.RouteRule{
		Type: "ip_cidr", Values: []string{"10.0.0.0/8"}, Action: "direct",
	}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteRuleRequiresKnownUpstream(t *testing.T) {
	ups := []proxy.UpstreamConfig{{Name: "default"}}
	err := validateRouteRule(proxy.RouteRule{
		Type: "domain", Values: []string{"example.com"}, Action: "proxy", Upstreams: []string{"missing"},
	}, ups)
	if err == nil || !strings.Contains(err.Error(), "unknown upstream") {
		t.Fatalf("expected unknown upstream error, got %v", err)
	}
	if err := validateRouteRule(proxy.RouteRule{
		Type: "domain", Values: []string{"example.com"}, Action: "proxy", Upstreams: []string{"default"},
	}, ups); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteRuleMatchNeedsNoValues(t *testing.T) {
	if err := validateRouteRule(proxy.RouteRule{Type: "match", Action: "direct"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateRouteRule(proxy.RouteRule{Type: "domain_suffix", Action: "direct"}, nil); err == nil {
		t.Fatal("expected error for missing values")
	}
}

func TestValidateUpstreamConfig(t *testing.T) {
	cases := []struct {
		name string
		up   proxy.UpstreamConfig
		ok   bool
	}{
		{"valid socks5", proxy.UpstreamConfig{URL: "socks5://127.0.0.1:1080", Types: []string{"socks5"}}, true},
		{"bad scheme", proxy.UpstreamConfig{URL: "ftp://127.0.0.1:21"}, false},
		{"bad port", proxy.UpstreamConfig{URL: "socks5://127.0.0.1:70000"}, false},
		{"bad strategy", proxy.UpstreamConfig{URL: "socks5://127.0.0.1:1080", Strategy: "random"}, false},
		{"bad health check", proxy.UpstreamConfig{
			URL:         "socks5://127.0.0.1:1080",
			HealthCheck: proxy.HealthCheckConfig{Type: "bogus"},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpstream(tc.up)
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNextUpstreamNameAvoidsCollision(t *testing.T) {
	ups := []proxy.UpstreamConfig{{Name: "upstream-2"}}
	if got := nextUpstreamName(ups); got != "upstream-3" {
		t.Fatalf("expected upstream-3, got %s", got)
	}
}
