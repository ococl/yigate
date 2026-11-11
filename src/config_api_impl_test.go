package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yigate/proxy"
)

func writeConfigForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestConfigEditor(t *testing.T, content string) (*configEditor, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	writeConfigForTest(t, cfgPath, content)
	applyCalled := false
	editor := newConfigEditor(cfgPath, func() error {
		applyCalled = true
		return nil
	})
	t.Cleanup(func() { _ = os.Remove(cfgPath) })
	_ = applyCalled
	return editor, cfgPath
}

func TestConfigEditorAddUpstream(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUpstream(proxy.UpstreamConfig{
		Name: "up2",
		URL:  "http://host2:8080",
	})
	if err != nil {
		t.Fatalf("AddUpstream: %v", err)
	}

	// Verify it was persisted
	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "up2") {
		t.Fatalf("expected up2 in config file, got:\n%s", content)
	}
	if !contains(content, "host2:8080") {
		t.Fatalf("expected host2:8080 in config file, got:\n%s", content)
	}
}

// TestConfigEditorUpstreamTLSPathsRelativeToConfig 覆盖 WebUI 用相对路径提交证书的场景：
// 校验基准必须是配置文件目录而不是进程工作目录，且落盘保留相对路径。
func TestConfigEditorUpstreamTLSPathsRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
upstreams: []
routing:
  final: auto
`)
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "certs", "ca.crt"), []byte("ca"), 0o600); err != nil {
		t.Fatal(err)
	}

	editor := newConfigEditor(cfgPath, func() error { return nil })
	// 测试进程的工作目录是包目录，certs/ca.crt 在其中并不存在，
	// 若校验仍以工作目录为基准，这里会直接失败。
	err := editor.AddUpstream(proxy.UpstreamConfig{
		Name:          "gm",
		URL:           "https://127.0.0.1:18443",
		Types:         []string{"https"},
		TLSProtocol:   "gm",
		TLSRootCAFile: "certs/ca.crt",
	})
	if err != nil {
		t.Fatalf("AddUpstream with relative tls_ca_file: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tls_ca_file: certs/ca.crt") {
		t.Fatalf("expected relative path to be persisted as-is, got:\n%s", data)
	}
}

func TestConfigEditorAddUpstreamDuplicate(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUpstream(proxy.UpstreamConfig{
		Name: "up1",
		URL:  "http://host2:8080",
	})
	if err == nil {
		t.Fatal("expected error for duplicate upstream")
	}
}

func TestConfigEditorRemoveUpstream(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
  - name: "up2"
    url: "http://host2:8080"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["example.com"]
      action: proxy
      upstreams: ["up1"]
`)

	err := editor.RemoveUpstream("up1")
	if err != nil {
		t.Fatalf("RemoveUpstream: %v", err)
	}

	// Verify upstream removed and rule references cleaned
	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "up1") {
		t.Fatalf("expected up1 to be removed from config, got:\n%s", content)
	}
}

func TestConfigEditorRemoveUpstreamNotFound(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveUpstream("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent upstream")
	}
}

func TestConfigEditorUpdateUpstream(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
    weight: 5
routing:
  final: auto
`)

	err := editor.UpdateUpstream("up1", proxy.UpstreamConfig{
		URL:    "socks5://newhost:1080",
		Weight: 10,
	})
	if err != nil {
		t.Fatalf("UpdateUpstream: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "newhost:1080") {
		t.Fatalf("expected newhost:1080 in config, got:\n%s", content)
	}
}

func TestConfigEditorAddRule(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddRule(proxy.RouteRule{
		Type:   "domain_suffix",
		Values: []string{"test.com"},
		Action: "proxy",
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "test.com") {
		t.Fatalf("expected test.com in config, got:\n%s", content)
	}
}

func TestConfigEditorReplaceRules(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["old.com"]
      action: direct
`)

	err := editor.ReplaceRules([]proxy.RouteRule{
		{Type: "geosite", Values: []string{"cn"}, Action: "direct"},
		{Type: "geosite", Values: []string{"geolocation-!cn"}, Action: "proxy", Upstreams: []string{"up1"}},
		{Type: "match", Action: "proxy"},
	})
	if err != nil {
		t.Fatalf("ReplaceRules: %v", err)
	}

	rules := editor.ListRules()
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].Type != "geosite" || rules[0].Values[0] != "cn" {
		t.Fatalf("unexpected first rule: %+v", rules[0])
	}
	if rules[1].Upstreams[0] != "up1" {
		t.Fatalf("expected upstream ref preserved, got %+v", rules[1])
	}
	if rules[2].Type != "match" {
		t.Fatalf("expected match rule last, got %+v", rules[2])
	}

	data, _ := os.ReadFile(cfgPath)
	if contains(string(data), "old.com") {
		t.Fatalf("expected old rule replaced, got:\n%s", data)
	}
}

// 任一条非法都不应落盘：整体替换必须保持原配置不变。
func TestConfigEditorReplaceRulesRejectsInvalid(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["old.com"]
      action: direct
`)

	err := editor.ReplaceRules([]proxy.RouteRule{
		{Type: "geosite", Values: []string{"cn"}, Action: "direct"},
		{Type: "domain", Action: "proxy"},
	})
	if err == nil {
		t.Fatal("expected error for rule without values")
	}
	rules := editor.ListRules()
	if len(rules) != 1 || len(rules[0].Values) == 0 || rules[0].Values[0] != "old.com" {
		t.Fatalf("rules must stay unchanged on validation failure, got %+v", rules)
	}
}

func TestConfigEditorSetRulesDisabled(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["a.com"]
      action: direct
    - type: domain_suffix
      values: ["b.com"]
      action: proxy
`)

	res, err := editor.SetRulesDisabled([]int{0, 7}, true)
	if err != nil {
		t.Fatalf("SetRulesDisabled: %v", err)
	}
	if len(res.Updated) != 1 || len(res.Failed) != 1 {
		t.Fatalf("expected 1 updated + 1 failed, got %+v", res)
	}
	rules := editor.ListRules()
	if !rules[0].Disabled {
		t.Fatalf("rule 0 should be disabled, got %+v", rules[0])
	}
	if rules[1].Disabled {
		t.Fatalf("rule 1 must stay enabled, got %+v", rules[1])
	}

	data, _ := os.ReadFile(cfgPath)
	if !contains(string(data), "disabled: true") {
		t.Fatalf("expected disabled flag persisted, got:\n%s", data)
	}

	// 再批量启用，disabled 字段应从 YAML 中消失（omitempty）
	if _, err := editor.SetRulesDisabled([]int{0}, false); err != nil {
		t.Fatalf("SetRulesDisabled(false): %v", err)
	}
	data, _ = os.ReadFile(cfgPath)
	if contains(string(data), "disabled:") {
		t.Fatalf("expected disabled field removed, got:\n%s", data)
	}
}

// 禁用规则允许引用暂不存在的上游：否则"想先停用、再重建上游"就做不成。
func TestConfigEditorDisabledRuleAllowsUnknownUpstream(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
`)

	err := editor.AddRule(proxy.RouteRule{
		Type:      "domain_suffix",
		Values:    []string{"x.com"},
		Action:    "proxy",
		Upstreams: []string{"missing"},
		Disabled:  true,
	})
	if err != nil {
		t.Fatalf("disabled rule should tolerate unknown upstream: %v", err)
	}

	err = editor.AddRule(proxy.RouteRule{
		Type:      "domain_suffix",
		Values:    []string{"y.com"},
		Action:    "proxy",
		Upstreams: []string{"missing"},
	})
	if err == nil {
		t.Fatal("enabled rule with unknown upstream should be rejected")
	}
}

func TestConfigEditorRemoveRule(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["first.com"]
      action: direct
    - type: domain_suffix
      values: ["second.com"]
      action: proxy
`)

	err := editor.RemoveRule(0)
	if err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "first.com") {
		t.Fatalf("expected first.com to be removed, got:\n%s", content)
	}
	if !contains(content, "second.com") {
		t.Fatalf("expected second.com to remain, got:\n%s", content)
	}
}

func TestConfigEditorRemoveRuleOutOfRange(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveRule(99)
	if err == nil {
		t.Fatal("expected error for out-of-range rule index")
	}
}

func TestConfigEditorAddUser(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "pass1"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUser("bob", "pass2")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "bob") {
		t.Fatalf("expected bob in config, got:\n%s", content)
	}
	if !contains(content, "pass2") {
		t.Fatalf("expected pass2 in config, got:\n%s", content)
	}
}

func TestConfigEditorAddUserDuplicate(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "pass1"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUser("alice", "newpass")
	if err == nil {
		t.Fatal("expected error for duplicate user")
	}
}

func TestConfigEditorRemoveUser(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "pass1"
  bob: "pass2"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveUser("alice", false)
	if err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "alice") {
		t.Fatalf("expected alice to be removed, got:\n%s", content)
	}
	if !contains(content, "bob") {
		t.Fatalf("expected bob to remain, got:\n%s", content)
	}
}

func TestConfigEditorRemoveUserNotFound(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "pass1"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveUser("nonexistent", false)
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

func TestConfigEditorListUpstreams(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
  - name: "up2"
    url: "http://host2:8080"
routing:
  final: auto
`)

	ups := editor.ListUpstreams()
	if len(ups) != 2 {
		t.Fatalf("expected 2 upstreams, got %d", len(ups))
	}
	if ups[0].Name != "up1" {
		t.Fatalf("expected first name up1, got %s", ups[0].Name)
	}
}

func TestConfigEditorListRules(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["example.com"]
      action: direct
`)

	rules := editor.ListRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Type != "domain_suffix" {
		t.Fatalf("expected type domain_suffix, got %s", rules[0].Type)
	}
}

func TestConfigEditorListUsers(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "pass1"
  bob: "pass2"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	users := editor.ListUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestConfigEditorMultiListenerListUsers(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listeners:
  - listen: "127.0.0.1:1080"
    users:
      alice: "pass1"
      carol: "pass3"
  - listen: "127.0.0.1:1081"
    users:
      bob: "pass2"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	users := editor.ListUsers()
	if len(users) != 3 {
		t.Fatalf("expected 3 users (alice, carol, bob), got %d: %v", len(users), users)
	}
	seen := make(map[string]bool)
	for _, u := range users {
		seen[u.Username] = true
	}
	for _, name := range []string{"alice", "bob", "carol"} {
		if !seen[name] {
			t.Errorf("expected user %s in list", name)
		}
	}
}

func TestConfigEditorMultiListenerAddUser(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listeners:
  - listen: "127.0.0.1:1080"
    users:
      alice: "pass1"
  - listen: "127.0.0.1:1081"
    users:
      bob: "pass2"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUser("charlie", "pass3")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "charlie") {
		t.Fatalf("expected charlie in config, got:\n%s", content)
	}
	// charlie 应出现在全局 users 和两个 listener 中
	// 至少出现 3 次（全局 + 2 个 listener）
	charlieCount := strings.Count(content, "charlie")
	if charlieCount < 2 {
		t.Fatalf("expected charlie in at least 2 places (global + listeners), got %d occurrences", charlieCount)
	}
}

func TestConfigEditorMultiListenerRemoveUser(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listeners:
  - listen: "127.0.0.1:1080"
    users:
      alice: "pass1"
      bob: "pass2"
  - listen: "127.0.0.1:1081"
    users:
      alice: "pass1"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveUser("alice", false)
	if err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "alice") {
		t.Fatalf("expected alice to be removed from all listeners, got:\n%s", content)
	}
	// bob 应保留
	if !contains(content, "bob") {
		t.Fatalf("expected bob to remain, got:\n%s", content)
	}
}

func TestConfigEditorMultiListenerRemoveUserNotFound(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listeners:
  - listen: "127.0.0.1:1080"
    users:
      alice: "pass1"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.RemoveUser("nonexistent", false)
	if err == nil {
		t.Fatal("expected error for non-existent user in multi-listener config")
	}
}

func TestConfigEditorPreservesEnvVars(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
users:
  alice: "${ALICE_PASSWORD}"
upstreams:
  - name: "up1"
    url: "socks5://${UPSTREAM_HOST}:1080"
routing:
  final: auto
`)

	// Add a new upstream - existing ${ENV_VAR} should be preserved
	err := editor.AddUpstream(proxy.UpstreamConfig{
		Name: "up2",
		URL:  "http://host2:8080",
	})
	if err != nil {
		t.Fatalf("AddUpstream: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !contains(content, "${ALICE_PASSWORD}") {
		t.Fatalf("expected ${ALICE_PASSWORD} to be preserved, got:\n%s", content)
	}
	if !contains(content, "${UPSTREAM_HOST}") {
		t.Fatalf("expected ${UPSTREAM_HOST} to be preserved, got:\n%s", content)
	}
}

func TestConfigEditorEmptyURL(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)

	err := editor.AddUpstream(proxy.UpstreamConfig{
		Name: "up2",
		URL:  "",
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfigEditorDisableUpstreamPersistsAndLeavesManagerPool(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
routing:
  final: auto
`)
	disabled := false
	if err := editor.UpdateUpstream("up1", proxy.UpstreamConfig{
		Name: "up1", URL: "socks5://host1:1080", Enabled: &disabled,
	}); err != nil {
		t.Fatalf("UpdateUpstream() error = %v", err)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "enabled: false") {
		t.Fatalf("config not persisted with enabled=false:\n%s", raw)
	}
	ups := editor.ListUpstreams()
	if len(ups) != 1 || ups[0].IsEnabled() {
		t.Fatalf("ListUpstreams() = %#v, want one disabled upstream", ups)
	}
	mgr, err := proxy.NewUpstreamManager(ups)
	if err != nil {
		t.Fatalf("NewUpstreamManager() error = %v", err)
	}
	if up := mgr.Select(); up != nil {
		t.Fatalf("Select() = %#v, want nil when all upstreams are disabled", up)
	}
}

// TestConfigEditorAutoGeneratesGeoSiteSyncToken 开启下游同步时由系统生成令牌，
// 用户无需也无法手写。
func TestConfigEditorAutoGeneratesGeoSiteSyncToken(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  geosite:
    auto_update: false
`)
	enabled := true
	if err := editor.UpdateSettings(proxy.SettingsPatch{GeoSiteSyncEnabled: &enabled}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Routing.GeoSite.SyncEnabled {
		t.Fatal("sync_enabled should be true")
	}
	token := cfg.Routing.GeoSite.SyncToken
	if len(token) != 64 {
		t.Fatalf("generated token length = %d, want 64", len(token))
	}
}

// TestConfigEditorClearsGeoSiteUpdateToken 显式写入空值会清除已保存的更新令牌，
// WebUI「恢复默认」依赖该语义把更新令牌一并恢复为空。
func TestConfigEditorClearsGeoSiteUpdateToken(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  geosite:
    auto_update: false
    update_token: "old-token"
`)
	empty := ""
	if err := editor.UpdateSettings(proxy.SettingsPatch{GeoSiteUpdateToken: &empty}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Routing.GeoSite.UpdateToken != "" {
		t.Fatalf("update_token = %q, want empty", cfg.Routing.GeoSite.UpdateToken)
	}
}

// TestConfigEditorRotateGeoSiteSyncToken 重新生成会替换旧令牌并写回配置。
func TestConfigEditorRotateGeoSiteSyncToken(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  geosite:
    auto_update: false
    sync_enabled: true
    sync_token: "old-token-12345678"
`)
	token, err := editor.RotateGeoSiteSyncToken()
	if err != nil {
		t.Fatalf("RotateGeoSiteSyncToken: %v", err)
	}
	if len(token) != 64 || contains(token, "old-token") {
		t.Fatalf("unexpected token %q", token)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Routing.GeoSite.SyncToken != token {
		t.Fatalf("persisted token = %q, want %q", cfg.Routing.GeoSite.SyncToken, token)
	}
}

// 批量删除的核心承诺：多条改动只落盘、只热加载一次，且索引按删除前的位置解释。
func TestConfigEditorRemoveUpstreamsWritesOnce(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
upstreams:
  - name: "up1"
    url: "socks5://host1:1080"
  - name: "up2"
    url: "http://host2:8080"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["example.com"]
      action: proxy
      upstreams: ["up1", "up2"]
`)
	applies := 0
	editor := newConfigEditor(cfgPath, func() error {
		applies++
		return nil
	})

	res, err := editor.RemoveUpstreams([]string{"up1", "up2", "ghost"})
	if err != nil {
		t.Fatalf("RemoveUpstreams: %v", err)
	}
	if applies != 1 {
		t.Fatalf("apply called %d times, want 1", applies)
	}
	if len(res.Deleted) != 2 {
		t.Fatalf("deleted = %v, want up1 and up2", res.Deleted)
	}
	if len(res.Failed) != 1 || res.Failed[0].Target != "ghost" {
		t.Fatalf("failed = %v, want ghost", res.Failed)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "up1") || contains(content, "up2") {
		t.Fatalf("expected upstreams removed from config, got:\n%s", content)
	}
	cfg, err := editor.loadRawConfig()
	if err != nil {
		t.Fatalf("loadRawConfig: %v", err)
	}
	if len(cfg.Routing.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(cfg.Routing.Rules))
	}
	if refs := cfg.Routing.Rules[0].Upstreams; len(refs) != 0 {
		t.Fatalf("rule upstream refs = %v, want empty", refs)
	}
}

func TestConfigEditorRemoveRulesUsesOriginalIndexes(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
routing:
  final: auto
  rules:
    - type: domain_suffix
      values: ["a.com"]
      action: direct
    - type: domain_suffix
      values: ["b.com"]
      action: direct
    - type: domain_suffix
      values: ["c.com"]
      action: direct
`)

	res, err := editor.RemoveRules([]int{0, 2, 9})
	if err != nil {
		t.Fatalf("RemoveRules: %v", err)
	}
	if len(res.Deleted) != 2 {
		t.Fatalf("deleted = %v, want indexes 0 and 2", res.Deleted)
	}
	if len(res.Failed) != 1 || res.Failed[0].Target != "9" {
		t.Fatalf("failed = %v, want index 9", res.Failed)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if contains(content, "a.com") || contains(content, "c.com") {
		t.Fatalf("expected first and third rules removed, got:\n%s", content)
	}
	if !contains(content, "b.com") {
		t.Fatalf("expected middle rule kept, got:\n%s", content)
	}
}

func TestConfigEditorRemoveUsersWritesOnce(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	writeConfigForTest(t, cfgPath, `listen: "127.0.0.1:0"
users:
  alice: "secret1"
  bob: "secret2"
routing:
  final: auto
`)
	applies := 0
	editor := newConfigEditor(cfgPath, func() error {
		applies++
		return nil
	})

	res, err := editor.RemoveUsers([]string{"alice", "bob", "nobody"}, false)
	if err != nil {
		t.Fatalf("RemoveUsers: %v", err)
	}
	if applies != 1 {
		t.Fatalf("apply called %d times, want 1", applies)
	}
	if len(res.Deleted) != 2 || len(res.Failed) != 1 {
		t.Fatalf("deleted = %v, failed = %v", res.Deleted, res.Failed)
	}

	data, _ := os.ReadFile(cfgPath)
	if contains(string(data), "alice") || contains(string(data), "bob") {
		t.Fatalf("expected users removed, got:\n%s", data)
	}
}
