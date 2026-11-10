package proxy

import "testing"

func TestUpstreamManagerInjectsSeparateCredentials(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://127.0.0.1:1080",
		Username: "alice",
		Password: "s3cret-pass",
	}})
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	up, ok := mgr.byName["up1"]
	if !ok {
		t.Fatalf("upstream up1 not registered")
	}
	if up.URL.User == nil {
		t.Fatalf("separate credentials should be injected into URL userinfo")
	}
	if got := up.URL.User.Username(); got != "alice" {
		t.Fatalf("username = %q, want alice", got)
	}
	if pw, _ := up.URL.User.Password(); pw != "s3cret-pass" {
		t.Fatalf("password = %q, want s3cret-pass", pw)
	}
}

func TestUpstreamManagerSeparateCredentialsOverrideURL(t *testing.T) {
	mgr, err := NewUpstreamManager([]UpstreamConfig{{
		Name:     "up1",
		URL:      "socks5h://old:oldpass@127.0.0.1:1080",
		Username: "new",
		Password: "newpass",
	}})
	if err != nil {
		t.Fatalf("NewUpstreamManager: %v", err)
	}
	up := mgr.byName["up1"]
	if up == nil {
		t.Fatalf("upstream up1 not registered")
	}
	if got := up.URL.User.Username(); got != "new" {
		t.Fatalf("username = %q, want new", got)
	}
	if pw, _ := up.URL.User.Password(); pw != "newpass" {
		t.Fatalf("password = %q, want newpass", pw)
	}
}
