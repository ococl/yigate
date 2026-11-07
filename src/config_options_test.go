package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"yigate/proxy"
)

func loadConfigFromYAML(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	writeConfigForTest(t, path, content)
	return LoadConfig(path)
}

func TestLoadConfigDNSDefaults(t *testing.T) {
	cfg, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
dns:
  type: doh
  doh_url: https://dns.example/dns-query
`)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// 文档与示例均声明 fallback 默认 true，未显式配置时必须生效。
	if !cfg.DNS.Fallback {
		t.Fatal("dns.fallback should default to true")
	}
	if cfg.DNS.Timeout != proxy.Duration(5*time.Second) {
		t.Fatalf("dns.timeout = %v, want 5s", cfg.DNS.Timeout)
	}
	if cfg.DNS.IPPrecedence != proxy.IPPrecedenceIPv4First {
		t.Fatalf("dns.ip_precedence = %q", cfg.DNS.IPPrecedence)
	}
}

func TestValidateConfigRejectsInvalidRoutingEnum(t *testing.T) {
	cases := map[string]string{
		"routing.final": `listen: "127.0.0.1:0"
routing:
  final: nonsense
`,
		"routing.auto.mode": `listen: "127.0.0.1:0"
routing:
  auto:
    mode: bogus_mode
`,
	}
	for want, content := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := loadConfigFromYAML(t, content)
			if err == nil {
				t.Fatalf("expected error mentioning %s", want)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q should mention %q", err, want)
			}
		})
	}
}

func TestValidateConfigAcceptsRoutingEnums(t *testing.T) {
	for _, final := range []string{"direct", "proxy", "auto", "deny", "AUTO"} {
		content := "listen: \"127.0.0.1:0\"\nrouting:\n  final: " + final + "\n"
		if _, err := loadConfigFromYAML(t, content); err != nil {
			t.Fatalf("final=%s should be accepted: %v", final, err)
		}
	}
	for _, mode := range []string{"direct_first", "proxy_first", "Proxy_First"} {
		content := "listen: \"127.0.0.1:0\"\nrouting:\n  auto:\n    mode: " + mode + "\n"
		if _, err := loadConfigFromYAML(t, content); err != nil {
			t.Fatalf("auto.mode=%s should be accepted: %v", mode, err)
		}
	}
}

func TestValidateConfigRejectsNegativeAccessControl(t *testing.T) {
	cases := map[string]string{
		"connections_per_second": `listen: "127.0.0.1:0"
access_control:
  rate_limit:
    connections_per_second: -1
`,
		"burst": `listen: "127.0.0.1:0"
access_control:
  rate_limit:
    burst: -2
`,
		"max_conns_per_user": `listeners:
  - listen: "127.0.0.1:0"
    access_control:
      max_conns_per_user: -1
`,
		"max_connections": `listeners:
  - listen: "127.0.0.1:0"
    max_connections: -1
`,
	}
	for want, content := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := loadConfigFromYAML(t, content)
			if err == nil {
				t.Fatalf("expected error for negative %s", want)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q should mention %q", err, want)
			}
		})
	}
}

func TestValidateConfigRejectsTrafficAuditNegativeQuotaWhenDisabled(t *testing.T) {
	_, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
traffic_audit:
  enabled: false
  per_user_quota: -1
`)
	if err == nil || !strings.Contains(err.Error(), "per_user_quota") {
		t.Fatalf("expected per_user_quota error, got %v", err)
	}
}

func TestValidateConfigRejectsInvalidManagementAddress(t *testing.T) {
	_, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
management: "not-an-address"
management_auth_token: "x"
`)
	if err == nil || !strings.Contains(err.Error(), "management") {
		t.Fatalf("expected management address error, got %v", err)
	}
}

func TestValidateConfigRejectsInvalidGeoSiteUpdateURL(t *testing.T) {
	_, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
routing:
  geosite:
    auto_update: true
    update_url: "file:///etc/passwd"
`)
	if err == nil || !strings.Contains(err.Error(), "update_url") {
		t.Fatalf("expected update_url error, got %v", err)
	}
}

func TestValidateConfigUpstreamURLEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"bad scheme", "ftp://host:21", "scheme"},
		{"missing host", "socks5://", "host"},
		{"bad port", "socks5://host:70000", "port"},
		{"valid env placeholder", "socks5h://${UPSTREAM_HOST}:${UPSTREAM_PORT}", ""},
		{"valid ipv6", "http://[::1]:8080", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpstreamConfig(proxy.UpstreamConfig{Name: "u", URL: tc.url})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateConfigReportsUpstreamIndex(t *testing.T) {
	_, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
upstreams:
  - name: ok
    url: socks5://host:1080
  - name: bad
    url: ftp://host:21
`)
	if err == nil || !strings.Contains(err.Error(), "upstreams[1]") {
		t.Fatalf("expected upstreams[1] in error, got %v", err)
	}
}

func TestValidateConfigTProxyMode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("transparent proxy only supported on linux")
	}
	base := `listeners:
  - listen: "127.0.0.1:0"
    tproxy:
      enabled: true
      mode: %s
`
	if _, err := loadConfigFromYAML(t, strings.Replace(base, "%s", "TPROXY", 1)); err != nil {
		t.Fatalf("uppercase TPROXY should be accepted: %v", err)
	}
	if _, err := loadConfigFromYAML(t, strings.Replace(base, "%s", "bogus", 1)); err == nil {
		t.Fatal("unknown tproxy mode should be rejected")
	}
}

func TestValidateConfigRejectsInvalidRouteRule(t *testing.T) {
	cases := map[string]string{
		"rule type": `listen: "127.0.0.1:0"
routing:
  rules:
    - type: bogus
      values: ["x"]
      action: proxy
`,
		"ip_cidr": `listen: "127.0.0.1:0"
routing:
  rules:
    - type: ip_cidr
      values: ["10.0.0.0/33"]
      action: direct
`,
		"rule action": `listen: "127.0.0.1:0"
routing:
  rules:
    - type: domain
      values: ["example.com"]
      action: bogus
`,
	}
	for want, content := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := loadConfigFromYAML(t, content)
			if err == nil || !strings.Contains(err.Error(), "routing.rules[0]") {
				t.Fatalf("expected routing.rules[0] error, got %v", err)
			}
		})
	}
}

func TestLoadConfigInheritsGlobalDefaultsIntoListeners(t *testing.T) {
	cfg, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
users:
  alice: pass
access_control:
  allowed_clients: ["10.0.0.0/8"]
  rate_limit:
    connections_per_second: 10
    burst: 20
  max_conns_per_user: 5
listeners:
  - listen: "127.0.0.1:8891"
  - listen: "127.0.0.1:8892"
    users: {}
`)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listeners[0].Users["alice"] != "pass" {
		t.Fatal("listener without users should inherit global users")
	}
	if len(cfg.Listeners[1].Users) != 0 {
		t.Fatal("explicit empty users should stay anonymous")
	}
	if len(cfg.Listeners[1].AccessControl.AllowedClients) != 1 ||
		cfg.Listeners[1].AccessControl.RateLimit.ConnectionsPerSecond != 10 {
		t.Fatalf("listener should inherit global access_control, got %+v", cfg.Listeners[1].AccessControl)
	}
}

func TestLoadConfigDoesNotOverrideListenerAccessControl(t *testing.T) {
	cfg, err := loadConfigFromYAML(t, `listen: "127.0.0.1:0"
access_control:
  rate_limit:
    connections_per_second: 10
listeners:
  - listen: "127.0.0.1:8891"
    access_control:
      max_conns_per_user: 3
`)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listeners[0].AccessControl.MaxConnsPerUser != 3 {
		t.Fatalf("listener access_control overwritten: %+v", cfg.Listeners[0].AccessControl)
	}
}
