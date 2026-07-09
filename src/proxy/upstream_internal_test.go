package proxy

import (
	"crypto/tls"
	"net/url"
	"testing"
)

func TestNormalizeProxyTypes(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"valid", []string{"http", "SOCKS5", " Https "}, []string{"http", "socks5", "https"}, false},
		{"dup", []string{"http", "http"}, []string{"http"}, false},
		{"invalid", []string{"ftp"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := normalizeProxyTypes(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok && len(tc.in) > 0 {
				t.Fatal("expected ok=true")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestValidateProxyTypes(t *testing.T) {
	if err := ValidateProxyTypes([]string{"http", "socks5"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProxyTypes([]string{"ws"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpstreamPrimaryProxyType(t *testing.T) {
	u := &Upstream{Types: []string{"socks5", "http"}}
	if got := u.primaryProxyType(); got != "socks5" {
		t.Fatalf("expected socks5, got %q", got)
	}

	u = &Upstream{URL: mustParseURL(t, "http://127.0.0.1:8080")}
	if got := u.primaryProxyType(); got != "http" {
		t.Fatalf("expected http, got %q", got)
	}

	var nilUp *Upstream
	if got := nilUp.primaryProxyType(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestUpstreamURLForType(t *testing.T) {
	u := &Upstream{URL: mustParseURL(t, "http://127.0.0.1:8080")}
	got := u.urlForType("https")
	if got.Scheme != "https" || got.Host != "127.0.0.1:8080" {
		t.Fatalf("unexpected url: %v", got)
	}

	if u.urlForType("") == nil {
		t.Fatal("expected non-nil url")
	}

	var nilUp *Upstream
	if nilUp.urlForType("http") != nil {
		t.Fatal("expected nil")
	}
}

func TestUpstreamWithProxyType(t *testing.T) {
	u := &Upstream{Name: "u", URL: mustParseURL(t, "http://127.0.0.1:8080")}
	got := u.withProxyType("https")
	if got.URL.Scheme != "https" {
		t.Fatalf("expected https, got %s", got.URL.Scheme)
	}
	if got.Name != "u" {
		t.Fatal("name mismatch")
	}
}

func TestUpstreamSupportsProxyType(t *testing.T) {
	u := &Upstream{Types: []string{"socks5h"}}
	if !u.supportsProxyType("socks5") {
		t.Fatal("expected support socks5 when socks5h listed")
	}
	if !u.supportsProxyType("socks5h") {
		t.Fatal("expected support socks5h")
	}
	if u.supportsProxyType("http") {
		t.Fatal("expected no http support")
	}

	var nilUp *Upstream
	if nilUp.supportsProxyType("http") {
		t.Fatal("expected nil upstream unsupported")
	}
}

func TestProxyAddress(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"https://example.com", "example.com:443"},
		{"socks5://example.com", "example.com:1080"},
		{"http://example.com", "example.com:80"},
	}
	for _, tc := range cases {
		u := mustParseURL(t, tc.url)
		if got := proxyAddress(u); got != tc.want {
			t.Fatalf("proxyAddress(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestTLSConfigForHost(t *testing.T) {
	cfg := tlsConfigForHost("example.com:443")
	if cfg.ServerName != "example.com" {
		t.Fatalf("expected example.com, got %q", cfg.ServerName)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatal("expected TLS 1.2 min")
	}

	cfg2 := tlsConfigForHost("127.0.0.1:443")
	if cfg2.ServerName != "" {
		t.Fatalf("expected empty SNI for IP, got %q", cfg2.ServerName)
	}

	cfg3 := tlsConfigForHost("badaddr")
	if cfg3.ServerName != "badaddr" {
		t.Fatalf("expected badaddr, got %q", cfg3.ServerName)
	}
}

func TestProxyCredentials(t *testing.T) {
	u := &Upstream{URL: mustParseURL(t, "http://user:pass@127.0.0.1:8080")}
	got := proxyCredentials(u)
	if got == "" {
		t.Fatal("expected credentials")
	}
	if proxyCredentials(nil) != "" {
		t.Fatal("expected empty for nil")
	}
}

func TestUpstreamManagerMarkDead(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "dead", URL: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.StartHealthCheck(3600)
	defer mgr.StopHealthCheck()

	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
	}
	_, _, err = mgr.DialProxy("127.0.0.1:1", nil)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !up.dead {
		t.Fatal("expected upstream marked dead")
	}
}

func TestUpstreamDialOne(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, up, err := mgr.DialOne(target, "http")
	if err != nil {
		t.Fatalf("DialOne: %v", err)
	}
	if up == nil || up.Name != "http" {
		t.Fatal("expected upstream http")
	}
	conn.Close()

	_, _, err = mgr.DialOne(target, "missing")
	if err == nil {
		t.Fatal("expected error for missing upstream")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
