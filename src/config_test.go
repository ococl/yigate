package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k5pw/proxy"
)

func TestLoadConfigWithRouting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	cfgYAML := `listen: 127.0.0.1:8891
upstreams:
  - name: u1
    url: socks5://127.0.0.1:1080
    types:
      - socks5
routing:
  final: auto
  geosite:
    path: data/geosite.dat
    auto_update: true
    update_url: https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat
  socks5h_redirect:
    enabled: true
    upstreams:
      - u1
  rules:
    - type: domain_suffix
      values:
        - example.com
      action: proxy
      upstreams:
        - u1
timeouts:
  dial: 5
  handshake: 5
  http_exchange: 60
  tunnel_idle: 300
`
	if err := os.WriteFile(path, []byte(cfgYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
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
	geositePath, err := filepath.Abs("geosite.dat")
	if err != nil || !fileExists(geositePath) {
		t.Skip("geosite.dat is not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	cfgYAML := fmt.Sprintf(`listen: 127.0.0.1:8891
routing:
  final: proxy
  geosite:
    path: %s
  rules:
    - type: geosite
      values:
        - CN
      action: direct
`, geositePath)
	if err := os.WriteFile(path, []byte(cfgYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

	cfg = defaultConfig()
	cfg.Management = "0.0.0.0:8892"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for public management without auth token")
	}
	cfg.ManagementAuthToken = "secret-token"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for public management with auth token: %v", err)
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

func TestLoadConfigPreservesExplicitUnlimitedConnections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:0\nmax_connections: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MaxConnections != 0 {
		t.Fatalf("max connections = %d, want unlimited (0)", cfg.MaxConnections)
	}
}

func TestValidateConfigRejectsInvalidDurations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"dial timeout", func(cfg *Config) { cfg.Timeouts.Dial = proxy.Duration(-time.Second) }},
		{"direct timeout", func(cfg *Config) { cfg.Routing.Auto.DirectTimeout = proxy.Duration(-time.Second) }},
		{"cache ttl", func(cfg *Config) { cfg.Routing.Auto.CacheTTL = proxy.Duration(-time.Second) }},
		{"geosite interval", func(cfg *Config) { cfg.Routing.GeoSite.UpdateInterval = proxy.Duration(-time.Second) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			tc.mutate(cfg)
			if err := validateConfig(cfg); err == nil {
				t.Fatal("expected duration validation error")
			}
		})
	}
}

func TestLoadConfigExpandsSensitiveEnv(t *testing.T) {
	t.Setenv("K5PW_MANAGEMENT_TOKEN", "mgmt-token")
	t.Setenv("K5PW_PROXY_PASSWORD", "proxy-pass")
	t.Setenv("UPSTREAM_HOST", "127.0.0.1")
	t.Setenv("UPSTREAM_PORT", "1080")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: "${K5PW_MANAGEMENT_TOKEN}"
users:
  alice: "${K5PW_PROXY_PASSWORD}"
upstreams:
  - name: default
    url: "socks5h://${UPSTREAM_HOST}:${UPSTREAM_PORT}"
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ManagementAuthToken != "mgmt-token" {
		t.Fatalf("management token = %q", cfg.ManagementAuthToken)
	}
	if cfg.Users["alice"] != "proxy-pass" {
		t.Fatalf("user password = %q", cfg.Users["alice"])
	}
	if cfg.Upstreams[0].URL != "socks5h://127.0.0.1:1080" {
		t.Fatalf("upstream url = %q", cfg.Upstreams[0].URL)
	}
}

func TestLoadConfigRejectsMissingSensitiveEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
management_auth_token: "${MISSING_TOKEN}"
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing management token env")
	}

	path = filepath.Join(dir, "config-users.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
users:
  alice: "${MISSING_PASSWORD}"
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing user password env")
	}

	path = filepath.Join(dir, "config-upstream.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
upstreams:
  - url: "socks5h://${MISSING_HOST}:1080"
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing upstream env")
	}
}

func TestLoadConfigRejectsEmptySensitiveEnv(t *testing.T) {
	t.Setenv("EMPTY_TOKEN", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
management_auth_token: "${EMPTY_TOKEN}"
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for empty management token env")
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
unknown_field: true
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for unknown YAML field")
	}
}

func TestLoadConfigParsesDurationsAndResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
timeouts:
  dial: 2s
  handshake: 3
  http_exchange: 4s
  tunnel_idle: 5
routing:
  geosite:
    path: data/geosite.dat
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Timeouts.Dial != proxy.Duration(2*time.Second) {
		t.Fatalf("dial timeout = %v", time.Duration(cfg.Timeouts.Dial))
	}
	if cfg.Timeouts.Handshake != proxy.Duration(3*time.Second) {
		t.Fatalf("handshake timeout = %v", time.Duration(cfg.Timeouts.Handshake))
	}
	if cfg.Timeouts.HTTPExchange != proxy.Duration(4*time.Second) {
		t.Fatalf("http timeout = %v", time.Duration(cfg.Timeouts.HTTPExchange))
	}
	if cfg.Timeouts.TunnelIdle != proxy.Duration(5*time.Second) {
		t.Fatalf("tunnel timeout = %v", time.Duration(cfg.Timeouts.TunnelIdle))
	}
	wantPath := filepath.Join(dir, "data", "geosite.dat")
	if cfg.Routing.GeoSite.Path != wantPath {
		t.Fatalf("geosite path = %q, want %q", cfg.Routing.GeoSite.Path, wantPath)
	}
}

func TestValidateConfigRejectsDuplicateUpstreamNames(t *testing.T) {
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{
		{Name: "same", URL: "http://127.0.0.1:8080"},
		{Name: "same", URL: "http://127.0.0.1:8081"},
	}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected duplicate upstream name error")
	}
}
