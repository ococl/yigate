package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataDomainsAreDeniedByDefault(t *testing.T) {
	p, err := NewDestinationPolicy(DestinationPolicyConfig{
		ProtectPrivate: true,
		DefaultAction:  ActionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"metadata.google.internal:80",
		"foo.instance-data:80",
		"metadata.aws:443",
		"node.metadata.packet.net:80",
	} {
		if err := p.CheckTarget(target); err == nil {
			t.Fatalf("metadata alias %q should be denied", target)
		}
	}
	if err := p.CheckTarget("example.com:443"); err != nil {
		t.Fatalf("ordinary domain should be allowed: %v", err)
	}
}

func TestValidateUpstreamHost(t *testing.T) {
	for _, host := range []string{
		"metadata.google.internal", "metadata.aws", "localhost",
		"0.0.0.0", "169.254.169.254", "ff02::1",
	} {
		if err := ValidateUpstreamHost(host); err == nil {
			t.Fatalf("unsafe upstream host %q should be rejected", host)
		}
	}
	for _, host := range []string{
		"127.0.0.1", "10.2.3.150", "proxy.internal", "github.com", "${UPSTREAM_HOST}",
	} {
		if err := ValidateUpstreamHost(host); err != nil {
			t.Fatalf("legitimate upstream host %q should be allowed: %v", host, err)
		}
	}
}

func TestGeoSiteRedirectPolicy(t *testing.T) {
	initial, err := http.NewRequest(http.MethodGet, "https://example.com/dlc.dat", nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := http.NewRequest(http.MethodGet, "https://cdn.example.com/dlc.dat", nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed.Header.Set("Authorization", "Bearer update-token")
	if err := geoSiteRedirectPolicy(allowed, []*http.Request{initial}); err != nil {
		t.Fatalf("cross-host public redirect should be allowed: %v", err)
	}
	if got := allowed.Header.Get("Authorization"); got != "" {
		t.Fatalf("authorization header must be stripped on cross-host redirect, got %q", got)
	}

	blocked, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := geoSiteRedirectPolicy(blocked, []*http.Request{initial}); err == nil {
		t.Fatal("redirect to link-local metadata address must be rejected")
	}
}

func TestSecurityHeadersOnAPIResponses(t *testing.T) {
	handler := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	for header, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestResolveBeforeUpstreamFlag(t *testing.T) {
	off, err := NewDestinationPolicy(DestinationPolicyConfig{ProtectPrivate: true, DefaultAction: ActionAllow})
	if err != nil {
		t.Fatal(err)
	}
	if off.ShouldResolveBeforeUpstream() {
		t.Fatal("pre-resolution must stay opt-in to preserve remote DNS semantics")
	}
	on, err := NewDestinationPolicy(DestinationPolicyConfig{
		ProtectPrivate:        true,
		DefaultAction:         ActionAllow,
		ResolveBeforeUpstream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !on.ShouldResolveBeforeUpstream() {
		t.Fatal("strict mode should enable pre-resolution")
	}
}
