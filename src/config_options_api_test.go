package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yigate/proxy"
)

func TestGetSettingsReportsEffectiveListenerValues(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:4479"
socks5:
  allow_bind: true
max_connections: 999
listeners:
  - listen: "127.0.0.1:8891"
    max_connections: 100
    socks5:
      allow_udp_associate: true
  - listen: "127.0.0.1:8892"
`)
	view, err := editor.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !strings.Contains(view.Listen, "127.0.0.1:8891") || !strings.Contains(view.Listen, "127.0.0.1:8892") {
		t.Fatalf("listen should list effective listeners, got %q", view.Listen)
	}
	if view.MaxConnections != 100 {
		t.Fatalf("max_connections = %d, want listener value 100", view.MaxConnections)
	}
	if view.Socks5AllowBind {
		t.Fatal("socks5_allow_bind should reflect the first listener (false)")
	}
	if !view.Socks5AllowUDPAssoc {
		t.Fatal("socks5_allow_udp_associate should reflect the first listener (true)")
	}
}

func TestUpdateSettingsPropagatesToListeners(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listeners:
  - listen: "127.0.0.1:8891"
    max_connections: 100
  - listen: "127.0.0.1:8892"
    max_connections: 200
`)
	maxConns := 500
	allowBind := true
	rate := 7
	burst := 9
	maxPerUser := 3
	err := editor.UpdateSettings(proxy.SettingsPatch{
		MaxConnections:     &maxConns,
		Socks5AllowBind:    &allowBind,
		AllowedClients:     []string{"10.0.0.0/8"},
		RateLimitPerSecond: &rate,
		RateLimitBurst:     &burst,
		MaxConnsPerUser:    &maxPerUser,
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	for i, lc := range cfg.Listeners {
		if lc.MaxConnections != 500 {
			t.Fatalf("listeners[%d].max_connections = %d, want 500", i, lc.MaxConnections)
		}
		if !lc.Socks5.AllowBind {
			t.Fatalf("listeners[%d].socks5.allow_bind not propagated", i)
		}
		if lc.AccessControl.RateLimit.ConnectionsPerSecond != 7 ||
			lc.AccessControl.RateLimit.Burst != 9 ||
			lc.AccessControl.MaxConnsPerUser != 3 ||
			len(lc.AccessControl.AllowedClients) != 1 {
			t.Fatalf("listeners[%d].access_control not propagated: %+v", i, lc.AccessControl)
		}
	}
}

// TestUpdateSettingsWorksWithEnvPlaceholders 覆盖运行时配置 API 的校验路径：
// 它读取的是未展开的原始 YAML，含 ${ENV} 的字段不能因无法解析而阻塞设置更新。
func TestUpdateSettingsWorksWithEnvPlaceholders(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "${YIGATE_LISTEN}"
management: "${YIGATE_MGMT}"
management_auth_token: "${YIGATE_TOKEN}"
tls:
  cert_file: "${YIGATE_CERT}"
  key_file: "${YIGATE_KEY}"
upstreams:
  - name: u1
    url: "${YIGATE_UPSTREAM_URL}"
routing:
  geosite:
    auto_update: true
    update_url: "${YIGATE_GEOSITE_URL}"
  rules:
    - type: domain
      values: ["${YIGATE_DOMAIN}"]
      action: proxy
`)
	level := "debug"
	if err := editor.UpdateSettings(proxy.SettingsPatch{LogLevel: &level}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, placeholder := range []string{"${YIGATE_LISTEN}", "${YIGATE_UPSTREAM_URL}", "${YIGATE_DOMAIN}"} {
		if !strings.Contains(string(data), placeholder) {
			t.Fatalf("placeholder %s should be preserved, got:\n%s", placeholder, data)
		}
	}
	if !strings.Contains(string(data), "level: debug") {
		t.Fatalf("log level not persisted:\n%s", data)
	}
}

func TestUpdateSettingsWebUIEnabled(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
webui:
  enabled: true
`)
	disabled := false
	if err := editor.UpdateSettings(proxy.SettingsPatch{WebUIEnabled: &disabled}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WebUI == nil || cfg.WebUI.Enabled {
		t.Fatal("webui.enabled should be persisted as false")
	}
}

// TestReloadAppliesWebUIAndPprof 覆盖 webui.enabled / enable_pprof 的热加载：
// 管理地址保持不变时必须原地生效，而不是因端口占用导致 reload 失败。
func TestReloadAppliesWebUIAndPprof(t *testing.T) {
	dir := t.TempDir()
	mgmtAddr := freeTCPAddr(t)
	cfgPath := filepath.Join(dir, "config.yml")
	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
management: "`+mgmtAddr+`"
management_auth_token: "token-1"
webui:
  enabled: true
`)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	s, err := NewServer(cfgPath, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = s.Start() }()
	t.Cleanup(s.Stop)
	waitForFirstListener(t, s)

	statusOf := func(path string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+mgmtAddr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer token-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	statusWithToken := func(path, token string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+mgmtAddr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := statusOf("/debug/pprof/"); got != http.StatusNotFound {
		t.Fatalf("/debug/pprof/ before enable = %d, want 404", got)
	}

	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
management: "`+mgmtAddr+`"
management_auth_token: "token-1"
enable_pprof: true
webui:
  enabled: true
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("reload enable_pprof: %v", err)
	}
	if got := statusOf("/debug/pprof/"); got != http.StatusOK {
		t.Fatalf("/debug/pprof/ after enable = %d, want 200", got)
	}

	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
management: "`+mgmtAddr+`"
management_auth_token: "token-1"
enable_pprof: true
webui:
  enabled: false
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("reload webui disabled: %v", err)
	}
	if got := statusOf("/"); got != http.StatusNotFound {
		t.Fatalf("/ with webui disabled = %d, want 404", got)
	}
	if got := statusOf("/health"); got != http.StatusOK {
		t.Fatalf("/health = %d, want 200", got)
	}

	// management_auth_token 热加载：旧令牌立即失效，新令牌生效。
	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
management: "`+mgmtAddr+`"
management_auth_token: "token-2"
enable_pprof: true
webui:
  enabled: false
`)
	if err := s.LoadAndApply(); err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if got := statusWithToken("/health", "token-1"); got != http.StatusUnauthorized {
		t.Fatalf("/health with old token = %d, want 401", got)
	}
	if got := statusWithToken("/health", "token-2"); got != http.StatusOK {
		t.Fatalf("/health with new token = %d, want 200", got)
	}
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
