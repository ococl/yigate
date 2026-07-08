package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewUpstreamManagerUsesConfiguredTypes(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{
		Name:  "multi",
		URL:   "http://127.0.0.1:8080",
		Types: []string{"http", "socks5", "socks5h", "socks5"},
	}})
	if err != nil {
		t.Fatalf("NewUpstreamManager() error = %v", err)
	}
	up := mgr.byName["multi"]
	if up == nil || !up.ExplicitTypes {
		t.Fatalf("upstream explicit types were not kept: %#v", up)
	}
	if !up.supportsProxyType(ProxyTypeHTTP) || !up.supportsProxyType(ProxyTypeSocks5) {
		t.Fatalf("configured types = %#v", up.proxyTypes())
	}
	if got := mgr.Socks5AttemptCount(nil); got != 1 {
		t.Fatalf("Socks5AttemptCount() = %d, want 1", got)
	}
}

func TestNewUpstreamManagerRejectsInvalidTypes(t *testing.T) {
	_, err := NewUpstreamManager([]UpstreamConfig{{URL: "http://127.0.0.1:8080", Types: []string{"ftp"}}})
	if err == nil {
		t.Fatal("NewUpstreamManager() accepted invalid proxy type")
	}
}

func TestUpstreamTypeProbeLoadsCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "upstream-types.txt")
	cache := `[
  {
    "name": "cached",
    "url": "http://127.0.0.1:8080",
    "types": ["http", "socks5h"],
    "source": "probe"
  }
]
`
	if err := os.WriteFile(cachePath, []byte(cache), 0644); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "cached", URL: "http://127.0.0.1:8080"}})
	if err != nil {
		t.Fatalf("NewUpstreamManager() error = %v", err)
	}
	prober := NewUpstreamTypeProber(UpstreamProbeConfig{Enabled: true, Delay: Duration(time.Hour), CachePath: cachePath}, mgr)
	prober.Start()
	defer prober.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		up := mgr.byName["cached"]
		if up.supportsProxyType(ProxyTypeSocks5) && up.typeSource() == "cache" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cached types were not applied: types=%#v source=%q", mgr.byName["cached"].proxyTypes(), mgr.byName["cached"].typeSource())
}

func TestResolveUpstreamProbePath(t *testing.T) {
	cfg := UpstreamProbeConfig{CachePath: "upstream-types.txt"}
	ResolveUpstreamProbePath(&cfg, "/tmp/k5pw")
	if cfg.CachePath != "/tmp/k5pw/upstream-types.txt" {
		t.Fatalf("CachePath = %q", cfg.CachePath)
	}
}
