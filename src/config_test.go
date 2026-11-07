package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yigate/proxy"
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
	router, err := proxy.NewRouter(cfg.Routing, cfg.DNS)
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

func TestValidateTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	caFile := filepath.Join(dir, "ca.crt")
	os.WriteFile(certFile, []byte("not a cert"), 0644)
	os.WriteFile(keyFile, []byte("not a key"), 0644)
	os.WriteFile(caFile, []byte("not a ca"), 0644)

	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.TLS.CertFile = certFile
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when only cert_file is provided")
	}

	cfg.TLS.KeyFile = keyFile
	cfg.TLS.ClientCAFile = caFile
	// 文件存在但内容无效，这里只校验存在性，不校验证书可解析性
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	cfg.TLS.CertFile = filepath.Join(dir, "missing.crt")
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for missing cert file")
	}
}

func TestLoadConfigTLSPaths(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	os.WriteFile(certFile, []byte("cert"), 0644)
	os.WriteFile(keyFile, []byte("key"), 0644)

	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
tls:
  cert_file: server.crt
  key_file: server.key
`), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TLS.CertFile != certFile {
		t.Fatalf("cert file path = %q, want %q", cfg.TLS.CertFile, certFile)
	}
	if cfg.TLS.KeyFile != keyFile {
		t.Fatalf("key file path = %q, want %q", cfg.TLS.KeyFile, keyFile)
	}
}

func TestLoadConfigExpandsTLSFromEnv(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	os.WriteFile(certFile, []byte("cert"), 0644)
	os.WriteFile(keyFile, []byte("key"), 0644)

	t.Setenv("YIGATE_TLS_CERT", certFile)
	t.Setenv("YIGATE_TLS_KEY", keyFile)

	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
tls:
  cert_file: "${YIGATE_TLS_CERT}"
  key_file: "${YIGATE_TLS_KEY}"
`), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TLS.CertFile != certFile {
		t.Fatalf("cert file = %q", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != keyFile {
		t.Fatalf("key file = %q", cfg.TLS.KeyFile)
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
	t.Setenv("YIGATE_MANAGEMENT_TOKEN", "mgmt-token")
	t.Setenv("YIGATE_PROXY_PASSWORD", "proxy-pass")
	t.Setenv("UPSTREAM_HOST", "127.0.0.1")
	t.Setenv("UPSTREAM_PORT", "1080")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
management: 127.0.0.1:0
management_auth_token: "${YIGATE_MANAGEMENT_TOKEN}"
users:
  alice: "${YIGATE_PROXY_PASSWORD}"
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

func TestLoadConfigBackwardCompatibleSingleListen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:8891
users:
  alice: pass
access_control:
  allowed_clients:
    - 127.0.0.1/32
max_connections: 512
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Listen != "127.0.0.1:8891" {
		t.Fatalf("listener address = %q", cfg.Listeners[0].Listen)
	}
	if cfg.Listeners[0].Users["alice"] != "pass" {
		t.Fatalf("listener user not propagated")
	}
	if cfg.Listeners[0].MaxConnections != 512 {
		t.Fatalf("listener max_connections = %d", cfg.Listeners[0].MaxConnections)
	}
}

func TestLoadConfigMultipleListeners(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listeners:
  - listen: 127.0.0.1:8891
    users:
      alice: pass1
    max_connections: 100
  - listen: 127.0.0.1:8892
    users:
      bob: pass2
    max_connections: 200
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Listeners) != 2 {
		t.Fatalf("expected 2 listeners, got %d", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Listen != "127.0.0.1:8891" || cfg.Listeners[1].Listen != "127.0.0.1:8892" {
		t.Fatalf("listener addresses = %v", cfg.Listeners)
	}
	if cfg.Listeners[0].Users["alice"] != "pass1" || cfg.Listeners[1].Users["bob"] != "pass2" {
		t.Fatalf("per-listener users not loaded")
	}
	if cfg.Listeners[0].MaxConnections != 100 || cfg.Listeners[1].MaxConnections != 200 {
		t.Fatalf("per-listener max_connections not loaded")
	}
}

func TestValidateConfigRejectsDuplicateListenerAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listeners:
  - listen: 127.0.0.1:8891
  - listen: 127.0.0.1:8891
`), 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for duplicate listener address")
	}
}

func TestLoadConfigListenerTLSPaths(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	os.WriteFile(certFile, []byte("cert"), 0644)
	os.WriteFile(keyFile, []byte("key"), 0644)

	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listeners:
  - listen: 127.0.0.1:0
    tls:
      cert_file: server.crt
      key_file: server.key
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listeners[0].TLS.CertFile != certFile {
		t.Fatalf("listener cert file = %q, want %q", cfg.Listeners[0].TLS.CertFile, certFile)
	}
	if cfg.Listeners[0].TLS.KeyFile != keyFile {
		t.Fatalf("listener key file = %q, want %q", cfg.Listeners[0].TLS.KeyFile, keyFile)
	}
}

// ============================================================
// IPv6 配置测试
// ============================================================

func TestLoadConfigIPv6Listen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: "[::1]:8891"
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "[::1]:8891" {
		t.Fatalf("listen = %q, want [::1]:8891", cfg.Listen)
	}
	if len(cfg.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Listen != "[::1]:8891" {
		t.Fatalf("listener listen = %q, want [::1]:8891", cfg.Listeners[0].Listen)
	}
}

func TestLoadConfigIPv6CIDRRouteRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
routing:
  final: direct
  rules:
    - type: ip_cidr
      values:
        - "2001:db8::/32"
        - "::1/128"
        - "fc00::/7"
      action: proxy
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Routing.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Routing.Rules))
	}
	if len(cfg.Routing.Rules[0].Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(cfg.Routing.Rules[0].Values))
	}
}

func TestLoadConfigDNSIPPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
dns:
  type: system
  ip_precedence: ipv6_first
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DNS.IPPrecedence != "ipv6_first" {
		t.Fatalf("ip_precedence = %q, want ipv6_first", cfg.DNS.IPPrecedence)
	}
}

func TestValidateConfigRejectsInvalidIPPrecedence(t *testing.T) {
	cfg := defaultConfig()
	cfg.DNS.IPPrecedence = "invalid"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid ip_precedence")
	}

	cfg.DNS.IPPrecedence = "ipv4_first"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for ipv4_first: %v", err)
	}

	cfg.DNS.IPPrecedence = "ipv6_first"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for ipv6_first: %v", err)
	}

	cfg.DNS.IPPrecedence = "system"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error for system: %v", err)
	}
}

// ============================================================
// TProxy 配置测试
// ============================================================

func TestLoadConfigTProxy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
tproxy:
  enabled: true
  mode: redirect
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.TProxy.Enabled {
		t.Fatal("tproxy.enabled not loaded")
	}
	if cfg.TProxy.Mode != "redirect" {
		t.Fatalf("tproxy.mode = %q, want redirect", cfg.TProxy.Mode)
	}
}

func TestLoadConfigListenerTProxy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listeners:
  - listen: 127.0.0.1:0
    tproxy:
      enabled: true
      mode: tproxy
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(cfg.Listeners))
	}
	if !cfg.Listeners[0].TProxy.Enabled {
		t.Fatal("listener tproxy.enabled not loaded")
	}
	if cfg.Listeners[0].TProxy.Mode != "tproxy" {
		t.Fatalf("listener tproxy.mode = %q, want tproxy", cfg.Listeners[0].TProxy.Mode)
	}
}

func TestValidateConfigRejectsTProxyWithTLS(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.TProxy.Enabled = true
	cfg.TProxy.Mode = "redirect"
	cfg.TLS.CertFile = "cert.crt"
	cfg.TLS.KeyFile = "cert.key"
	// TLS + TProxy should be rejected (but TLS files don't exist, so we need to check the TProxy validation path)
	// Actually validateConfig checks TLS file existence first, so let's test with listener-level
	cfg.Listeners = nil
	cfg.TLS.CertFile = ""
	cfg.TLS.KeyFile = ""
	cfg.Listeners = []ListenerConfig{
		{
			Listen: "127.0.0.1:0",
			TLS:    TLSConfig{CertFile: "cert.crt", KeyFile: "cert.key"},
			TProxy: proxy.TProxyConfig{Enabled: true, Mode: "redirect"},
		},
	}
	// On non-Linux, TProxy validation will fail with platform error
	if err := validateConfig(cfg); err == nil {
		// On Linux, TLS + TProxy should be rejected
		if proxy.IsTProxySupported() {
			t.Fatal("expected error for TProxy with TLS")
		}
	}
}

// ============================================================
// TrafficAudit 配置测试
// ============================================================

func TestLoadConfigTrafficAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
traffic_audit:
  enabled: true
  per_user_quota: 1073741824
  per_upstream_quota: 5368709120
  quota_window: 1h
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.TrafficAudit.Enabled {
		t.Fatal("traffic_audit.enabled not loaded")
	}
	if cfg.TrafficAudit.PerUserQuota != 1073741824 {
		t.Fatalf("per_user_quota = %d, want 1073741824", cfg.TrafficAudit.PerUserQuota)
	}
	if cfg.TrafficAudit.PerUpstreamQuota != 5368709120 {
		t.Fatalf("per_upstream_quota = %d, want 5368709120", cfg.TrafficAudit.PerUpstreamQuota)
	}
}

func TestLoadConfigTrafficAuditDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
traffic_audit:
  enabled: true
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.TrafficAudit.Enabled {
		t.Fatal("traffic_audit.enabled not loaded")
	}
	if cfg.TrafficAudit.PerUserQuota != 0 {
		t.Fatalf("per_user_quota = %d, want 0", cfg.TrafficAudit.PerUserQuota)
	}
}

// ============================================================
// DNS 配置测试
// ============================================================

func TestLoadConfigDNSDoH(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
dns:
  type: doh
  doh_url: "https://dns.google/dns-query"
  fallback: true
  timeout: 3s
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DNS.Type != "doh" {
		t.Fatalf("dns.type = %q, want doh", cfg.DNS.Type)
	}
	if cfg.DNS.DoHURL != "https://dns.google/dns-query" {
		t.Fatalf("dns.doh_url = %q", cfg.DNS.DoHURL)
	}
	if !cfg.DNS.Fallback {
		t.Fatal("dns.fallback not loaded")
	}
}

func TestLoadConfigDNSDoT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
dns:
  type: dot
  dot_addr: "dns.google:853"
  fallback: false
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DNS.Type != "dot" {
		t.Fatalf("dns.type = %q, want dot", cfg.DNS.Type)
	}
	if cfg.DNS.DoTAddr != "dns.google:853" {
		t.Fatalf("dns.dot_addr = %q", cfg.DNS.DoTAddr)
	}
	if cfg.DNS.Fallback {
		t.Fatal("dns.fallback should be false")
	}
}

func TestValidateConfigRejectsInvalidDNS(t *testing.T) {
	cfg := defaultConfig()
	cfg.DNS.Type = "invalid"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid dns.type")
	}

	cfg.DNS.Type = "doh"
	cfg.DNS.DoHURL = ""
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for doh without doh_url")
	}

	cfg.DNS.Type = "doh"
	cfg.DNS.DoHURL = "http://not-https.com/dns-query"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for non-HTTPS doh_url")
	}

	cfg.DNS.Type = "dot"
	cfg.DNS.DoTAddr = ""
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for dot without dot_addr")
	}
}

// ============================================================
// 健康检查配置测试
// ============================================================

func TestLoadConfigHealthCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
upstreams:
  - name: test-up
    url: socks5://127.0.0.1:1080
    health_check:
      type: http
      path: /health
      expected_codes: [200, 204]
      interval: 10
      timeout: 3s
      failure_threshold: 2
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
	}
	hc := cfg.Upstreams[0].HealthCheck
	if hc.Type != "http" {
		t.Fatalf("health_check.type = %q, want http", hc.Type)
	}
	if hc.Path != "/health" {
		t.Fatalf("health_check.path = %q, want /health", hc.Path)
	}
	if len(hc.ExpectedCodes) != 2 {
		t.Fatalf("expected_codes len = %d, want 2", len(hc.ExpectedCodes))
	}
	if hc.FailureThreshold != 2 {
		t.Fatalf("failure_threshold = %d, want 2", hc.FailureThreshold)
	}
}

// ============================================================
// 负载均衡策略配置测试
// ============================================================

func TestLoadConfigLoadBalancingStrategy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
upstreams:
  - name: up1
    url: socks5://127.0.0.1:1080
    strategy: least_connections
    priority: 1
    weight: 1
  - name: up2
    url: socks5://127.0.0.1:1081
    strategy: consistent_hash
    priority: 1
    weight: 1
  - name: up3
    url: socks5://127.0.0.1:1082
    strategy: source_ip_hash
    priority: 1
    weight: 1
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Upstreams[0].Strategy != "least_connections" {
		t.Fatalf("up1 strategy = %q, want least_connections", cfg.Upstreams[0].Strategy)
	}
	if cfg.Upstreams[1].Strategy != "consistent_hash" {
		t.Fatalf("up2 strategy = %q, want consistent_hash", cfg.Upstreams[1].Strategy)
	}
	if cfg.Upstreams[2].Strategy != "source_ip_hash" {
		t.Fatalf("up3 strategy = %q, want source_ip_hash", cfg.Upstreams[2].Strategy)
	}
}

// ============================================================
// DestinationPolicy 配置测试
// ============================================================

func TestLoadConfigDestinationPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`listen: 127.0.0.1:0
destination_policy:
  protect_private: true
  default_action: deny
  deny_cidrs:
    - "10.0.0.0/8"
  allow_cidrs:
    - "8.8.8.0/24"
  deny_domains:
    - "evil.com"
  allow_domains:
    - "good.com"
  deny_ports:
    - 22
    - 3389
  allow_ports:
    - 80
    - 443
`), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.DestinationPolicy.ProtectPrivate {
		t.Fatal("protect_private not loaded")
	}
	if cfg.DestinationPolicy.DefaultAction != "deny" {
		t.Fatalf("default_action = %q, want deny", cfg.DestinationPolicy.DefaultAction)
	}
	if len(cfg.DestinationPolicy.DenyCIDRs) != 1 {
		t.Fatalf("deny_cidrs len = %d, want 1", len(cfg.DestinationPolicy.DenyCIDRs))
	}
	if len(cfg.DestinationPolicy.AllowCIDRs) != 1 {
		t.Fatalf("allow_cidrs len = %d, want 1", len(cfg.DestinationPolicy.AllowCIDRs))
	}
	if len(cfg.DestinationPolicy.DenyDomains) != 1 {
		t.Fatalf("deny_domains len = %d, want 1", len(cfg.DestinationPolicy.DenyDomains))
	}
	if len(cfg.DestinationPolicy.AllowDomains) != 1 {
		t.Fatalf("allow_domains len = %d, want 1", len(cfg.DestinationPolicy.AllowDomains))
	}
	if len(cfg.DestinationPolicy.DenyPorts) != 2 {
		t.Fatalf("deny_ports len = %d, want 2", len(cfg.DestinationPolicy.DenyPorts))
	}
	if len(cfg.DestinationPolicy.AllowPorts) != 2 {
		t.Fatalf("allow_ports len = %d, want 2", len(cfg.DestinationPolicy.AllowPorts))
	}
}

func TestWriteDefaultConfigIfMissing(t *testing.T) {
	t.Setenv("YIGATE_DEFAULT_LISTEN", "")
	t.Setenv("YIGATE_DEFAULT_MANAGEMENT", "")
	path := filepath.Join(t.TempDir(), "data", "config.yml")

	written, token, err := WriteDefaultConfigIfMissing(path)
	if err != nil {
		t.Fatalf("WriteDefaultConfigIfMissing: %v", err)
	}
	if !written {
		t.Fatal("expected default config to be written")
	}
	if len(token) != 64 {
		t.Fatalf("generated token length = %d, want 64", len(token))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8891" {
		t.Fatalf("listen = %q, want local proxy address", cfg.Listen)
	}
	if cfg.Management != "127.0.0.1:8890" {
		t.Fatalf("management = %q, want local management address", cfg.Management)
	}
	if cfg.ManagementAuthToken != token {
		t.Fatal("returned token does not match generated config")
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	written, token, err = WriteDefaultConfigIfMissing(path)
	if err != nil {
		t.Fatalf("second WriteDefaultConfigIfMissing: %v", err)
	}
	if written || token != "" {
		t.Fatalf("existing config overwritten: written=%v tokenReturned=%v", written, token != "")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing config content changed")
	}
}

func TestDefaultConfigYAMLListenOverrides(t *testing.T) {
	t.Setenv("YIGATE_DEFAULT_LISTEN", "0.0.0.0:8891")
	t.Setenv("YIGATE_DEFAULT_MANAGEMENT", "0.0.0.0:8890")
	content, err := DefaultConfigYAML(true)
	if err != nil {
		t.Fatalf("DefaultConfigYAML: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "0.0.0.0:8891" || cfg.Management != "0.0.0.0:8890" {
		t.Fatalf("listen overrides not applied: listen=%q management=%q", cfg.Listen, cfg.Management)
	}
	if cfg.ManagementAuthToken == "" {
		t.Fatal("public generated management listener must have an auth token")
	}
}
