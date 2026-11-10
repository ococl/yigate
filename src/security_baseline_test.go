package main

import (
	"strings"
	"testing"
)

func TestSecurityBaselineRejectsAnonymousNonLoopback(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listeners = []ListenerConfig{{Listen: "0.0.0.0:8891"}}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "allow_anonymous_proxy") {
		t.Fatalf("expected anonymous proxy baseline error, got %v", err)
	}
	cfg.AllowAnonymousProxy = true
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("explicit risk acknowledgement should be accepted: %v", err)
	}
}

func TestSecurityBaselineRejectsAnonymousPprof(t *testing.T) {
	cfg := defaultConfig()
	cfg.Management = "127.0.0.1:8890"
	cfg.EnablePprof = true
	if err := validateConfig(cfg); err == nil {
		t.Fatal("pprof without a management token must be rejected even on loopback")
	}
	cfg.ManagementAuthToken = "strong-management-token-2026"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("pprof with token should be accepted: %v", err)
	}
}

func TestSecurityBaselineRejectsWeakCredentials(t *testing.T) {
	for _, weak := range []string{"123456", "password", "admin", "token", "__replace_me__"} {
		cfg := defaultConfig()
		cfg.Users = map[string]UserConfig{"alice": {Password: weak}}
		if err := validateConfig(cfg); err == nil {
			t.Fatalf("weak credential %q should be rejected", weak)
		}
	}
	cfg := defaultConfig()
	cfg.Users = map[string]UserConfig{"alice": {Password: "Str0ng-Pass-2026"}}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("strong credential should be accepted: %v", err)
	}
}

func TestSecurityBaselineAllowsExplicitPrivateTargets(t *testing.T) {
	cfg := defaultConfig()
	cfg.DestinationPolicy.ProtectPrivate = false
	cfg.AllowPrivateTargets = true
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("explicitly acknowledged private targets should be allowed: %v", err)
	}
}
