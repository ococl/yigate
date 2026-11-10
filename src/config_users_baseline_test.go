package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"yigate/proxy"
)

// listen 故意使用非回环地址：只有这种端口在失去全部认证手段时才会触发匿名代理基线。
const lastUserBaselineConfig = `listen: "0.0.0.0:8891"
users:
  alice: "Str0ng-Pass-2026"
routing:
  final: auto
`

func readConfigFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

func TestRemoveLastUserRequiresConfirmation(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, lastUserBaselineConfig)
	before := readConfigFile(t, cfgPath)

	err := editor.RemoveUser("alice", false)
	if !errors.Is(err, proxy.ErrLastActiveUser) {
		t.Fatalf("expected ErrLastActiveUser, got %v", err)
	}
	if after := readConfigFile(t, cfgPath); after != before {
		t.Fatalf("rejected removal must not touch config:\n%s", after)
	}
}

func TestRemoveLastUserWithForceFallsBackToAnonymous(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, lastUserBaselineConfig)

	if err := editor.RemoveUser("alice", true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	content := readConfigFile(t, cfgPath)
	if strings.Contains(content, "alice") {
		t.Fatalf("alice should be removed:\n%s", content)
	}
	if !strings.Contains(content, "allow_anonymous_proxy: true") {
		t.Fatalf("force removal must enable anonymous proxy fallback:\n%s", content)
	}
	// 单监听配置不应被展开成 listeners 数组元素（保存时只做校验，不重排形态）。
	if strings.Contains(content, "- listen:") {
		t.Fatalf("delete must keep the original single-listen shape:\n%s", content)
	}
}

func TestDisableLastUserRequiresConfirmation(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, lastUserBaselineConfig)
	before := readConfigFile(t, cfgPath)

	err := editor.SetUserEnabled("alice", false, false)
	if !errors.Is(err, proxy.ErrLastActiveUser) {
		t.Fatalf("expected ErrLastActiveUser, got %v", err)
	}
	if after := readConfigFile(t, cfgPath); after != before {
		t.Fatalf("rejected disable must not touch config:\n%s", after)
	}
}

func TestDisableLastUserWithForceFallsBackToAnonymous(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, lastUserBaselineConfig)

	if err := editor.SetUserEnabled("alice", false, true); err != nil {
		t.Fatalf("force disable: %v", err)
	}
	content := readConfigFile(t, cfgPath)
	if !strings.Contains(content, "enabled: false") {
		t.Fatalf("disabled user must be persisted with enabled: false:\n%s", content)
	}
	if !strings.Contains(content, "allow_anonymous_proxy: true") {
		t.Fatalf("force disable must enable anonymous proxy fallback:\n%s", content)
	}
}

func TestDisableUserKeepsOtherUserActive(t *testing.T) {
	editor, _ := newTestConfigEditor(t, `listen: "0.0.0.0:8891"
users:
  alice: "Str0ng-Pass-2026"
  bob: "An0ther-Pass-2026"
routing:
  final: auto
`)

	if err := editor.SetUserEnabled("alice", false, false); err != nil {
		t.Fatalf("disabling alice should be allowed while bob stays active: %v", err)
	}
	users := editor.ListUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %v", users)
	}
	for _, u := range users {
		if u.Username == "alice" && u.Enabled {
			t.Fatalf("alice should be reported as disabled: %v", users)
		}
		if u.Username == "bob" && !u.Enabled {
			t.Fatalf("bob should stay enabled: %v", users)
		}
	}
}

func TestUserScalarFormParsesAsEnabled(t *testing.T) {
	editor, _ := newTestConfigEditor(t, lastUserBaselineConfig)

	users := editor.ListUsers()
	if len(users) != 1 || users[0].Username != "alice" || !users[0].Enabled {
		t.Fatalf("legacy scalar form must parse as an enabled user, got %v", users)
	}
}

func TestEnabledUserIsExcludedFromAuth(t *testing.T) {
	cfg := defaultConfig()
	cfg.Users = map[string]UserConfig{
		"alice": {Password: "Str0ng-Pass-2026"},
	}
	disabled := false
	cfg.Users["bob"] = UserConfig{Password: "An0ther-Pass-2026", Enabled: &disabled}

	active := ActiveUsers(cfg.Users)
	if _, ok := active["bob"]; ok {
		t.Fatalf("disabled user must be excluded from authentication: %v", active)
	}
	if active["alice"] != "Str0ng-Pass-2026" {
		t.Fatalf("enabled user should be kept: %v", active)
	}
}
