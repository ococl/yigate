package proxy

import (
	"net/url"
	"testing"
)

func TestNewUpstreamManagerAcceptsSocks5H(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "remote-dns", URL: "socks5h://127.0.0.1:1080"}})
	if err != nil {
		t.Fatalf("NewUpstreamManager() error = %v", err)
	}
	if got := mgr.Socks5AttemptCount(nil); got != 1 {
		t.Fatalf("Socks5AttemptCount() = %d, want 1", got)
	}
	if up := mgr.SelectSocks5For(nil); up == nil || up.Name != "remote-dns" {
		t.Fatalf("SelectSocks5For() = %#v, want remote-dns", up)
	}
}

func TestNewUpstreamManagerRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewUpstreamManager([]UpstreamConfig{{URL: "ftp://127.0.0.1:21"}})
	if err == nil {
		t.Fatal("NewUpstreamManager() accepted unsupported scheme")
	}
}

func TestUpstreamManagerSelectAndWeighted(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "a", URL: "http://127.0.0.1:8080", Priority: 1, Weight: 2},
		{Name: "b", URL: "http://127.0.0.1:8081", Priority: 1, Weight: 1},
		{Name: "c", URL: "http://127.0.0.1:8082", Priority: 2, Weight: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mgr.SelectFor([]string{"a"}); got == nil || got.Name != "a" {
		t.Fatal("expected SelectFor a")
	}
	if got := mgr.SelectFor([]string{"missing"}); got != nil {
		t.Fatal("expected nil for missing upstream")
	}

	// Priority: should pick priority 1 first
	seen := map[string]int{}
	for i := 0; i < 30; i++ {
		up := mgr.Select()
		seen[up.Name]++
	}
	if seen["c"] > 0 {
		t.Fatal("expected priority 1 selected before priority 2")
	}
	if seen["a"] == 0 || seen["b"] == 0 {
		t.Fatalf("expected both a and b selected, got %v", seen)
	}
}

func TestUpstreamManagerDuplicateName(t *testing.T) {
	_, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "same", URL: "http://127.0.0.1:8080"},
		{Name: "same", URL: "http://127.0.0.1:8081"},
	})
	if err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestUpstreamManagerDefaultNameAndWeight(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{URL: "http://127.0.0.1:8080"}})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up.Name != "upstream-1" || up.Weight != 1 {
		t.Fatalf("unexpected default name/weight: %s/%d", up.Name, up.Weight)
	}
}

func TestUpstreamProxyTypes(t *testing.T) {
	up := &Upstream{URL: mustURL("socks5h://127.0.0.1:1080"), Types: []string{"socks5h"}}
	if !up.supportsProxyType("socks5") {
		t.Fatal("socks5h should support socks5")
	}
	if up.primaryProxyType() != "socks5h" {
		t.Fatalf("primary type = %s", up.primaryProxyType())
	}
	if up.urlForType("socks5").Scheme != "socks5" {
		t.Fatal("urlForType failed")
	}
	if up.withProxyType("socks5").URL.Scheme != "socks5" {
		t.Fatal("withProxyType failed")
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
