package proxy

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDurationUnmarshalYAML(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want time.Duration
	}{
		{"empty", "d: 0\n", 0},
		{"int", "d: 30\n", 30 * time.Second},
		{"duration", "d: 2h30m\n", 2*time.Hour + 30*time.Minute},
		{"duration_string", "d: 1500ms\n", 1500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s struct {
				D Duration `yaml:"d"`
			}
			if err := yaml.Unmarshal([]byte(tc.yaml), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if time.Duration(s.D) != tc.want {
				t.Fatalf("got %v, want %v", time.Duration(s.D), tc.want)
			}
		})
	}

	t.Run("invalid", func(t *testing.T) {
		var s struct {
			D Duration `yaml:"d"`
		}
		if err := yaml.Unmarshal([]byte("d: not-a-duration\n"), &s); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveRoutingPaths(t *testing.T) {
	dir := t.TempDir()
	rel := "geo.dat"
	abs := filepath.Join(dir, "geo2.dat")

	cfg := &RoutingConfig{GeoSite: GeoSiteConfig{Path: rel}}
	ResolveRoutingPaths(cfg, dir)
	if cfg.GeoSite.Path != filepath.Join(dir, rel) {
		t.Fatalf("expected relative resolved, got %q", cfg.GeoSite.Path)
	}

	cfg2 := &RoutingConfig{GeoSite: GeoSiteConfig{Path: abs}}
	ResolveRoutingPaths(cfg2, "/elsewhere")
	if cfg2.GeoSite.Path != abs {
		t.Fatalf("expected abs unchanged, got %q", cfg2.GeoSite.Path)
	}

	ResolveRoutingPaths(nil, dir)
}

func TestRouterDialProxyCache(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RoutingConfig{Final: ActionProxy}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	target := startMockTarget(t)
	conn, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	conn.Close()

	// cached path
	conn2, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial proxy cached: %v", err)
	}
	conn2.Close()
}

func TestRouterDialAutoDirectFirst(t *testing.T) {
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})
	router, err := NewRouter(RoutingConfig{Final: ActionAuto, Auto: AutoConfig{Mode: "direct_first", DirectTimeout: Duration(500 * time.Millisecond)}}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	target := startMockTarget(t)
	conn, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial auto direct: %v", err)
	}
	conn.Close()

	// cache hit
	conn2, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial auto cached: %v", err)
	}
	conn2.Close()
}

func TestRouterDialAutoProxyFirst(t *testing.T) {
	proxyAddr := startMockHTTPProxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "http", URL: "http://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RoutingConfig{Final: ActionAuto, Auto: AutoConfig{Mode: "proxy_first", DirectTimeout: Duration(100 * time.Millisecond)}}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	target := startMockTarget(t)
	conn, err := router.Dial(target, mgr)
	if err != nil {
		t.Fatalf("dial auto proxy first: %v", err)
	}
	conn.Close()
}

func TestRouterDialAutoProxyFallback(t *testing.T) {
	mgr, _ := NewUpstreamManager([]UpstreamConfig{})
	router, err := NewRouter(RoutingConfig{Final: ActionAuto, Auto: AutoConfig{Mode: "direct_first", DirectTimeout: Duration(100 * time.Millisecond)}}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = router.Dial("127.0.0.1:1", mgr)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRouterDialSocks5H(t *testing.T) {
	proxyAddr := startMockSocks5Proxy(t)
	mgr, err := NewUpstreamManager([]UpstreamConfig{{Name: "socks5", URL: "socks5://" + proxyAddr}})
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RoutingConfig{
		Final:           ActionDirect,
		Socks5HRedirect: Socks5HConfig{Enabled: true, Upstreams: []string{"socks5"}},
		Auto:            AutoConfig{CacheTTL: Duration(time.Hour)},
	}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}

	target := startMockTarget(t)
	conn, err := router.DialSocks5H(target, mgr)
	if err != nil {
		t.Fatalf("DialSocks5H: %v", err)
	}
	conn.Close()

	// cached path
	conn2, err := router.DialSocks5H(target, mgr)
	if err != nil {
		t.Fatalf("DialSocks5H cached: %v", err)
	}
	conn2.Close()
}

func TestRouterNewRouterInvalid(t *testing.T) {
	_, err := NewRouter(RoutingConfig{Final: "invalid"}, DNSConfig{})
	if err == nil {
		t.Fatal("expected error for invalid final")
	}
}

func TestRouterDialDirectNilManager(t *testing.T) {
	router, err := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, err := router.Dial(target, nil)
	if err != nil {
		t.Fatalf("dial direct: %v", err)
	}
	conn.Close()
}

func TestDialTargetForRequest(t *testing.T) {
	router, err := NewRouter(RoutingConfig{Final: ActionDirect}, DNSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	target := startMockTarget(t)
	conn, err := dialTargetForRequest(target, nil, router)
	if err != nil {
		t.Fatalf("dial target for request: %v", err)
	}
	conn.Close()
}

func TestDialTargetTimeout(t *testing.T) {
	conn, err := DialTargetTimeout("127.0.0.1:1", 100*time.Millisecond)
	if err == nil {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("expected timeout error")
	}
	if _, ok := err.(net.Error); !ok {
		t.Fatalf("expected net error, got %T", err)
	}
}

func TestLoadGeoSiteDatErrorCases(t *testing.T) {
	_, err := LoadGeoSiteDat(filepath.Join(t.TempDir(), "missing.dat"), []string{"CN"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadGeoSiteDatInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.dat")
	if err := os.WriteFile(path, []byte("not protobuf"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadGeoSiteDat(path, []string{"CN"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestNewRouterGeoSiteMissingFile verifies that NewRouter does not fail when
// geosite.dat is missing. Instead, geosite rules are silently disabled.
func TestNewRouterGeoSiteMissingFile(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionDirect,
		GeoSite: GeoSiteConfig{
			Path: filepath.Join(t.TempDir(), "nonexistent.dat"),
		},
		Rules: []RouteRule{
			{Type: "geosite", Values: []string{"CN"}, Action: ActionDirect},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter should not fail on missing geosite.dat: %v", err)
	}
	// geosite rule should not match since matcher is nil
	if got := r.Decide("www.qq.com:443").Action; got != ActionDirect {
		t.Fatalf("expected final direct (geosite disabled), got %q", got)
	}
}

// TestNewRouterGeoSiteEmptyPath verifies that NewRouter does not fail when
// geosite rules are used but path is empty.
func TestNewRouterGeoSiteEmptyPath(t *testing.T) {
	r, err := NewRouter(RoutingConfig{
		Final: ActionProxy,
		Rules: []RouteRule{
			{Type: "geosite", Values: []string{"CN"}, Action: ActionDirect},
		},
	}, DNSConfig{})
	if err != nil {
		t.Fatalf("NewRouter should not fail on empty geosite path: %v", err)
	}
	// geosite rule should not match; should fall through to final=proxy
	if got := r.Decide("www.qq.com:443").Action; got != ActionProxy {
		t.Fatalf("expected final proxy (geosite disabled), got %q", got)
	}
}

func TestHasGeoSiteRules(t *testing.T) {
	if HasGeoSiteRules(nil) {
		t.Fatal("expected false for nil rules")
	}
	if HasGeoSiteRules([]RouteRule{}) {
		t.Fatal("expected false for empty rules")
	}
	if !HasGeoSiteRules([]RouteRule{{Type: "geosite", Values: []string{"CN"}}}) {
		t.Fatal("expected true for geosite rule")
	}
	if HasGeoSiteRules([]RouteRule{{Type: "domain", Values: []string{"example.com"}}}) {
		t.Fatal("expected false for non-geosite rule")
	}
}
