package proxy

import (
	"net"
	"testing"
	"time"
)

func TestNewAccessController(t *testing.T) {
	cfg := AccessControlConfig{
		AllowedClients: []string{"127.0.0.1/32", "10.0.0.0/8"},
		RateLimit: struct {
			ConnectionsPerSecond int `yaml:"connections_per_second"`
			Burst                int `yaml:"burst"`
		}{ConnectionsPerSecond: 10, Burst: 5},
		MaxConnsPerUser: 2,
	}
	ac, err := NewAccessController(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ac == nil {
		t.Fatal("expected non-nil controller")
	}

	if _, err := NewAccessController(AccessControlConfig{AllowedClients: []string{"invalid"}}); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestAccessControllerIsAllowedIP(t *testing.T) {
	ac, _ := NewAccessController(AccessControlConfig{AllowedClients: []string{"127.0.0.0/8"}})
	if !ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}) {
		t.Fatal("expected 127.0.0.1 allowed")
	}
	if ac.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}) {
		t.Fatal("expected 192.168.1.1 denied")
	}

	open, _ := NewAccessController(AccessControlConfig{})
	if !open.IsAllowedIP(&net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}) {
		t.Fatal("expected allow all when whitelist empty")
	}
}

func TestAccessControllerAllowConn(t *testing.T) {
	cfg := AccessControlConfig{
		RateLimit: struct {
			ConnectionsPerSecond int `yaml:"connections_per_second"`
			Burst                int `yaml:"burst"`
		}{ConnectionsPerSecond: 1, Burst: 1},
	}
	ac, _ := NewAccessController(cfg)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	if !ac.AllowConn(addr) {
		t.Fatal("expected first conn allowed")
	}
	if ac.AllowConn(addr) {
		t.Fatal("expected second conn denied due to bucket empty")
	}

	ac2, _ := NewAccessController(AccessControlConfig{})
	if !ac2.AllowConn(addr) {
		t.Fatal("expected allow when rate limit disabled")
	}
}

func TestAccessControllerUserSlots(t *testing.T) {
	cfg := AccessControlConfig{MaxConnsPerUser: 2}
	ac, _ := NewAccessController(cfg)
	if !ac.AcquireUserSlot("alice") {
		t.Fatal("expected first slot")
	}
	if !ac.AcquireUserSlot("alice") {
		t.Fatal("expected second slot")
	}
	if ac.AcquireUserSlot("alice") {
		t.Fatal("expected third slot denied")
	}
	ac.ReleaseUserSlot("alice")
	if !ac.AcquireUserSlot("alice") {
		t.Fatal("expected slot after release")
	}
	ac.ReleaseUserSlot("alice")
	ac.ReleaseUserSlot("alice") // should not go negative
	ac.ReleaseUserSlot("bob")   // unknown user should be no-op

	open, _ := NewAccessController(AccessControlConfig{})
	if !open.AcquireUserSlot("alice") || !open.AcquireUserSlot("alice") {
		t.Fatal("expected unlimited when max conns disabled")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	lim := newRateLimiter(100, 1)
	lim.tokens = 0
	lim.last = time.Now().Add(-time.Second)
	if !lim.allow() {
		t.Fatal("expected token refill after elapsed time")
	}
}

func TestAccessControllerCleanupLifecycle(t *testing.T) {
	ac, err := NewAccessController(AccessControlConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ac.StartCleanup(time.Millisecond, time.Millisecond)
	ac.StartCleanup(time.Millisecond, time.Millisecond)
	done := make(chan struct{})
	go func() {
		ac.StopCleanup()
		ac.StopCleanup()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup goroutine did not stop")
	}
}

func TestCleanupStaleRateLimiters(t *testing.T) {
	cfg := AccessControlConfig{RateLimit: struct {
		ConnectionsPerSecond int `yaml:"connections_per_second"`
		Burst                int `yaml:"burst"`
	}{ConnectionsPerSecond: 10, Burst: 5}}
	ac, _ := NewAccessController(cfg)
	addr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1234}
	ac.AllowConn(addr)
	ac.mu.Lock()
	if len(ac.rateLimiters) != 1 {
		ac.mu.Unlock()
		t.Fatalf("expected 1 limiter, got %d", len(ac.rateLimiters))
	}
	for _, lim := range ac.rateLimiters {
		lim.mu.Lock()
		lim.lastUsed = time.Now().Add(-time.Hour)
		lim.mu.Unlock()
	}
	ac.mu.Unlock()
	ac.CleanupStaleRateLimiters(time.Minute)
	ac.mu.Lock()
	count := len(ac.rateLimiters)
	ac.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 stale limiters after cleanup, got %d", count)
	}
}
