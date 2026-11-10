package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"yigate/proxy"
)

func TestValidateUpstreamRejectsPartialCredentials(t *testing.T) {
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://127.0.0.1:1080",
		Username: "alice",
	}}
	if err := validateConfig(cfg); err == nil || !strings.Contains(err.Error(), "password must not be empty") {
		t.Fatalf("username without password should be rejected, got %v", err)
	}

	cfg.Upstreams = []proxy.UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://127.0.0.1:1080",
		Password: "s3cret-pass",
	}}
	if err := validateConfig(cfg); err == nil || !strings.Contains(err.Error(), "username must not be empty") {
		t.Fatalf("password without username should be rejected, got %v", err)
	}
}

func TestValidateUpstreamRejectsCredentialConflict(t *testing.T) {
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://embedded:pass@127.0.0.1:1080",
		Username: "alice",
		Password: "s3cret-pass",
	}}
	if err := validateConfig(cfg); err == nil || !strings.Contains(err.Error(), "must not embed credentials") {
		t.Fatalf("url embedded credentials plus separate fields should conflict, got %v", err)
	}
}

func TestValidateUpstreamAcceptsSeparateCredentials(t *testing.T) {
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://127.0.0.1:1080",
		Username: "alice",
		Password: "s3cret-pass",
	}}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("separate credentials should be accepted: %v", err)
	}
}

func TestUpstreamPasswordSupportsEnvPlaceholder(t *testing.T) {
	t.Setenv("YIGATE_TEST_UPSTREAM_PASSWORD", "env-secret-pass")
	cfg := defaultConfig()
	cfg.Upstreams = []proxy.UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://127.0.0.1:1080",
		Username: "alice",
		Password: "${YIGATE_TEST_UPSTREAM_PASSWORD}",
	}}
	if err := expandConfigEnv(cfg, nil); err != nil {
		t.Fatalf("expandConfigEnv: %v", err)
	}
	if got := cfg.Upstreams[0].Password; got != "env-secret-pass" {
		t.Fatalf("password = %q, want env-secret-pass", got)
	}
}

func TestUserEnabledRoundTripsThroughYAML(t *testing.T) {
	disabled := false
	cfg := defaultConfig()
	cfg.Users = map[string]UserConfig{
		"alice": {Password: "Str0ng-Pass-2026"},
		"bob":   {Password: "An0ther-Pass-2026", Enabled: &disabled},
	}

	path := t.TempDir() + "/config.yml"
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reloaded, err := loadConfig(path, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reloaded.Users["alice"].IsEnabled() {
		t.Fatalf("alice should stay enabled after round-trip")
	}
	if reloaded.Users["bob"].IsEnabled() {
		t.Fatalf("bob should stay disabled after round-trip")
	}
	if reloaded.Users["bob"].Password != "An0ther-Pass-2026" {
		t.Fatalf("bob password lost: %q", reloaded.Users["bob"].Password)
	}
}
