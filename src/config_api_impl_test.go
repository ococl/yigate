package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k5pw/proxy"
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

	err := editor.RemoveUser("alice")
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

	err := editor.RemoveUser("nonexistent")
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
		seen[u] = true
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

	err := editor.RemoveUser("alice")
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

	err := editor.RemoveUser("nonexistent")
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
