package proxy

import (
	"net"
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

func TestUpstreamManagerLeastConnectionsStrategy(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "a", URL: "http://127.0.0.1:8080", Strategy: StrategyLeastConnections},
		{Name: "b", URL: "http://127.0.0.1:8081", Strategy: StrategyLeastConnections},
	})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up.Name != "a" {
		t.Fatalf("expected a first (equal conns), got %s", up.Name)
	}
	up.activeConns.Add(1)
	up = mgr.Select()
	if up.Name != "b" {
		t.Fatalf("expected b (fewer conns), got %s", up.Name)
	}
}

func TestUpstreamManagerConsistentHashStrategy(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "a", URL: "http://127.0.0.1:8080", Strategy: StrategyConsistentHash},
		{Name: "b", URL: "http://127.0.0.1:8081", Strategy: StrategyConsistentHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	up1 := mgr.selectFrom(nil, "example.com:443", "")
	up2 := mgr.selectFrom(nil, "example.com:443", "")
	if up1.Name != up2.Name {
		t.Fatalf("consistent hash should return same upstream for same key, got %s and %s", up1.Name, up2.Name)
	}
	up3 := mgr.selectFrom(nil, "other.com:443", "")
	if up3 == nil {
		t.Fatal("expected upstream for different key")
	}
}

func TestUpstreamManagerSourceIPHashStrategy(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "a", URL: "http://127.0.0.1:8080", Strategy: StrategySourceIPHash},
		{Name: "b", URL: "http://127.0.0.1:8081", Strategy: StrategySourceIPHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	up1 := mgr.selectFrom(nil, "", "10.0.0.1")
	up2 := mgr.selectFrom(nil, "", "10.0.0.1")
	if up1.Name != up2.Name {
		t.Fatalf("source ip hash should return same upstream for same source IP, got %s and %s", up1.Name, up2.Name)
	}
	up3 := mgr.selectFrom(nil, "", "10.0.0.2")
	if up3 == nil {
		t.Fatal("expected upstream for different source IP")
	}
}

func TestUpstreamManagerRejectsInvalidStrategy(t *testing.T) {
	_, err := NewUpstreamManager([]UpstreamConfig{{URL: "http://127.0.0.1:8080", Strategy: "magic"}})
	if err == nil {
		t.Fatal("expected invalid strategy error")
	}
}

func TestUpstreamTrackConn(t *testing.T) {
	up := &Upstream{}
	c1, c2 := net.Pipe()
	wrapped := up.trackConn(c1)
	if up.ActiveConns() != 1 {
		t.Fatalf("active conns = %d", up.ActiveConns())
	}
	wrapped.Close()
	c2.Close()
	if up.ActiveConns() != 0 {
		t.Fatalf("active conns after close = %d", up.ActiveConns())
	}
}

func TestUpstreamManagerSourceIPHashFallback(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{
		{Name: "a", URL: "http://127.0.0.1:8080", Strategy: StrategySourceIPHash},
		{Name: "b", URL: "http://127.0.0.1:8081", Strategy: StrategySourceIPHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	up := mgr.Select()
	if up == nil {
		t.Fatal("expected upstream")
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
