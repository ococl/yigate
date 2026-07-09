package main

import (
	"os"
	"path/filepath"
	"testing"

	"k5pw/proxy"
)

func TestLoadConfigWithRouting(t *testing.T) {
	cfg, err := LoadConfig("config.yml")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Routing.Final != "auto" {
		t.Fatalf("routing final = %q, want %q", cfg.Routing.Final, "auto")
	}
	if cfg.Routing.GeoSite.Path == "" {
		t.Fatal("routing geosite path was not loaded")
	}
	if !cfg.Routing.GeoSite.AutoUpdate {
		t.Fatal("routing geosite auto update was not enabled")
	}
	if cfg.Routing.GeoSite.UpdateURL == "" {
		t.Fatal("routing geosite update url was not defaulted")
	}
	if cfg.Routing.GeoSite.UpdateURL != "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat" {
		t.Fatalf("routing geosite update url = %q", cfg.Routing.GeoSite.UpdateURL)
	}
	if len(cfg.Upstreams) == 0 || len(cfg.Upstreams[0].Types) == 0 {
		t.Fatalf("upstream types were not loaded: %#v", cfg.Upstreams)
	}
	if cfg.Timeouts.Dial == 0 {
		t.Fatal("timeouts.dial was not loaded")
	}
	if cfg.Timeouts.Handshake == 0 {
		t.Fatal("timeouts.handshake was not loaded")
	}
	if cfg.Timeouts.HTTPExchange == 0 {
		t.Fatal("timeouts.http_exchange was not loaded")
	}
	if cfg.Timeouts.TunnelIdle == 0 {
		t.Fatal("timeouts.tunnel_idle was not loaded")
	}
	if !cfg.Routing.Socks5HRedirect.Enabled {
		t.Fatal("routing socks5h redirect was not enabled")
	}
	if len(cfg.Routing.Rules) == 0 {
		t.Fatal("routing rules were not loaded")
	}
}

func TestConfigGeoSiteCNRouting(t *testing.T) {
	if _, err := os.Stat("geosite.dat"); err != nil {
		t.Skip("geosite.dat is not available")
	}

	cfg, err := LoadConfig("config.yml")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	router, err := proxy.NewRouter(cfg.Routing)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	if got := router.Decide("www.qq.com:443").Action; got != proxy.ActionDirect {
		t.Fatalf("www.qq.com action = %q, want %q", got, proxy.ActionDirect)
	}
	if got := router.Decide("www.baidu.com:443").Action; got != proxy.ActionDirect {
		t.Fatalf("www.baidu.com action = %q, want %q", got, proxy.ActionDirect)
	}
	if got := router.Decide("github.com:443").Action; got != proxy.ActionProxy {
		t.Fatalf("github.com action = %q, want %q", got, proxy.ActionProxy)
	}
}

func TestValidateConfigListen(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "not-a-valid-address"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid listen address")
	}

	cfg.Listen = "127.0.0.1:8891"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for valid listen address: %v", err)
	}
}

func TestValidateConfigUpstreamReferences(t *testing.T) {
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{
		{Name: "hk", URL: "socks5://127.0.0.1:1080"},
		{Name: "sg", URL: "socks5://127.0.0.1:1081"},
	}
	cfg.Routing.Rules = []proxy.RouteRule{
		{Type: "domain_suffix", Values: []string{"example.com"}, Action: "proxy", Upstreams: []string{"hk"}},
	}
	cfg.Routing.Socks5HRedirect.Upstreams = []string{"sg"}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for valid upstream references: %v", err)
	}

	cfg.Routing.Rules[0].Upstreams = []string{"unknown"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for unknown upstream reference in rule")
	}

	cfg.Routing.Rules[0].Upstreams = []string{"hk"}
	cfg.Routing.Socks5HRedirect.Upstreams = []string{"unknown"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for unknown upstream reference in socks5h_redirect")
	}
}

func TestValidateConfigErrors(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = ""
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for empty listen")
	}

	cfg = defaultConfig()
	cfg.CheckInterval = -1
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for negative check interval")
	}

	cfg = defaultConfig()
	cfg.MaxConnections = -1
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for negative max connections")
	}

	cfg = defaultConfig()
	cfg.Log.Level = "invalid"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid log level")
	}

	cfg = defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{URL: ""}}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for empty upstream URL")
	}

	cfg = defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{URL: "http://127.0.0.1:8080", Weight: -1}}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for negative weight")
	}

	cfg = defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{URL: "http://127.0.0.1:8080", Types: []string{"unknown"}}}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid proxy type")
	}

	cfg = defaultConfig()
	cfg.AccessControl.AllowedClients = []string{"invalid"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/config.yml"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte("listen: 127.0.0.1:0\n"), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:0" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.MaxConnections != defaultMaxConnections {
		t.Fatalf("max connections default = %d", cfg.MaxConnections)
	}
}
